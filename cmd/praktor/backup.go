package main

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/moby/moby/api/pkg/stdcopy"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const (
	volumePrefix       = "praktor-"
	defaultHelperImage = "alpine:3"
)

func runBackup(args []string) error {
	var outputPath string
	var helperImage string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-f":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for -f")
			}
			i++
			outputPath = args[i]
		case "-image":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for -image")
			}
			i++
			helperImage = args[i]
		}
	}

	if outputPath == "" {
		fmt.Fprintf(os.Stderr, "Usage: praktor backup -f <output.tar.zst> [-image <helper-image>]\n")
		return fmt.Errorf("missing -f flag")
	}
	if helperImage == "" {
		helperImage = defaultHelperImage
	}

	ctx := context.Background()
	docker, err := client.New(client.FromEnv)
	if err != nil {
		return fmt.Errorf("docker client: %w", err)
	}
	defer func() { _ = docker.Close() }()

	volumes, err := listPraktorVolumes(ctx, docker)
	if err != nil {
		return fmt.Errorf("list volumes: %w", err)
	}

	if len(volumes) == 0 {
		slog.Warn("no praktor volumes found, creating empty archive")
	}

	// Ensure helper image is available
	if err := ensureImage(ctx, docker, helperImage); err != nil {
		return fmt.Errorf("pull helper image: %w", err)
	}

	// Create output file
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	zw, err := zstd.NewWriter(f)
	if err != nil {
		return fmt.Errorf("create zstd writer: %w", err)
	}
	defer func() { _ = zw.Close() }()

	tw := tar.NewWriter(zw)
	defer func() { _ = tw.Close() }()

	for _, vol := range volumes {
		slog.Info("backing up volume", "name", vol)
		if err := backupVolume(ctx, docker, tw, vol, helperImage); err != nil {
			return fmt.Errorf("backup volume %s: %w", vol, err)
		}
	}

	// Close everything explicitly to catch write errors
	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close zstd: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}

	info, _ := os.Stat(outputPath)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}

	fmt.Printf("Backup complete: %d volumes, %s\n", len(volumes), formatSize(size))
	return nil
}

func backupVolume(ctx context.Context, docker *client.Client, tw *tar.Writer, volName, image string) error {
	containerName := fmt.Sprintf("praktor-backup-%d", time.Now().UnixNano())

	resp, err := docker.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     &dockercontainer.Config{Image: image, Entrypoint: []string{"true"}},
		HostConfig: &dockercontainer.HostConfig{Binds: []string{volName + ":/vol"}},
		Name:       containerName,
	})
	if err != nil {
		return fmt.Errorf("create temp container: %w", err)
	}
	defer func() {
		_, _ = docker.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
	}()

	copyResp, err := docker.CopyFromContainer(ctx, resp.ID, client.CopyFromContainerOptions{SourcePath: "/vol/."})
	if err != nil {
		return fmt.Errorf("copy from container: %w", err)
	}
	defer func() { _ = copyResp.Content.Close() }()

	// Re-write tar entries with volume name prefix
	srcTar := tar.NewReader(copyResp.Content)
	for {
		hdr, err := srcTar.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		// Prefix entry name with volume name
		hdr.Name = path.Join(volName, hdr.Name)
		if hdr.Typeflag == tar.TypeDir && !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write tar header: %w", err)
		}

		if hdr.Size > 0 {
			if _, err := io.Copy(tw, srcTar); err != nil {
				return fmt.Errorf("write tar data: %w", err)
			}
		}
	}

	return nil
}

func runRestore(args []string) error {
	var inputPath string
	var helperImage string
	overwrite := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-f":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for -f")
			}
			i++
			inputPath = args[i]
		case "-overwrite":
			overwrite = true
		case "-image":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for -image")
			}
			i++
			helperImage = args[i]
		}
	}

	if inputPath == "" {
		fmt.Fprintf(os.Stderr, "Usage: praktor restore -f <backup.tar.zst> [-overwrite] [-image <helper-image>]\n")
		return fmt.Errorf("missing -f flag")
	}
	if helperImage == "" {
		helperImage = defaultHelperImage
	}

	ctx := context.Background()
	docker, err := client.New(client.FromEnv)
	if err != nil {
		return fmt.Errorf("docker client: %w", err)
	}
	defer func() { _ = docker.Close() }()

	// Pre-scan: collect volume names from archive
	volumeNames, err := scanArchiveVolumes(inputPath)
	if err != nil {
		return fmt.Errorf("scan archive: %w", err)
	}

	if len(volumeNames) == 0 {
		fmt.Println("Archive contains no volumes.")
		return nil
	}

	// Check for existing volumes
	if !overwrite {
		existing, err := listPraktorVolumes(ctx, docker)
		if err != nil {
			return fmt.Errorf("list volumes: %w", err)
		}
		existingSet := make(map[string]bool, len(existing))
		for _, v := range existing {
			existingSet[v] = true
		}
		for _, name := range volumeNames {
			if existingSet[name] {
				return fmt.Errorf("volume %s already exists, add -overwrite to replace files", name)
			}
		}
	}

	// Ensure helper image is available
	if err := ensureImage(ctx, docker, helperImage); err != nil {
		return fmt.Errorf("pull helper image: %w", err)
	}

	// Restore phase: re-open and stream into volumes
	f, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	zr, err := zstd.NewReader(f)
	if err != nil {
		return fmt.Errorf("create zstd reader: %w", err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)

	// Track current volume's streaming state. Restore streams the tar into
	// the stdin of a helper container running `tar -xf - -C /vol`. This
	// bypasses the daemon's `PUT /containers/{id}/archive` validator, which
	// rejects entries with symlink targets that escape the destination
	// (common in nix store contents — e.g. `../../../../../etc/environment`).
	var (
		currentVol  string
		volTW       *tar.Writer
		attach      client.ContainerAttachResult
		waitResult  client.ContainerWaitResult
		drainStderr *bytes.Buffer
		drainDone   chan struct{}
		containerID string
	)

	// finishVolume closes the tar stream, waits for the helper container's
	// tar process to exit, captures its stderr, and removes the container.
	// Returns the helper's error (non-zero exit, stderr) if any.
	finishVolume := func() error {
		if volTW == nil {
			return nil
		}
		_ = volTW.Close()
		_ = attach.CloseWrite()

		var exitErr error
		select {
		case res := <-waitResult.Result:
			if res.Error != nil && res.Error.Message != "" {
				exitErr = fmt.Errorf("container error: %s", res.Error.Message)
			} else if res.StatusCode != 0 {
				exitErr = fmt.Errorf("tar exit %d: %s", res.StatusCode, strings.TrimSpace(drainStderr.String()))
			}
		case err := <-waitResult.Error:
			exitErr = err
		}

		<-drainDone
		attach.Close()
		_, _ = docker.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true})

		volTW = nil
		if exitErr != nil {
			return fmt.Errorf("restore %s: %w", currentVol, exitErr)
		}
		return nil
	}

	startVolume := func(volName string) error {
		_, err := docker.VolumeCreate(ctx, client.VolumeCreateOptions{Name: volName})
		if err != nil {
			return fmt.Errorf("create volume %s: %w", volName, err)
		}

		ctrName := fmt.Sprintf("praktor-restore-%d", time.Now().UnixNano())
		resp, err := docker.ContainerCreate(ctx, client.ContainerCreateOptions{
			Config: &dockercontainer.Config{
				Image:        helperImage,
				Cmd:          []string{"tar", "-xf", "-", "-C", "/vol"},
				OpenStdin:    true,
				StdinOnce:    true,
				AttachStdin:  true,
				AttachStdout: true,
				AttachStderr: true,
			},
			HostConfig: &dockercontainer.HostConfig{Binds: []string{volName + ":/vol"}},
			Name:       ctrName,
		})
		if err != nil {
			return fmt.Errorf("create temp container: %w", err)
		}
		containerID = resp.ID

		a, err := docker.ContainerAttach(ctx, containerID, client.ContainerAttachOptions{
			Stream: true, Stdin: true, Stdout: true, Stderr: true,
		})
		if err != nil {
			_, _ = docker.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true})
			return fmt.Errorf("attach %s: %w", volName, err)
		}
		attach = a

		if _, err := docker.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); err != nil {
			attach.Close()
			_, _ = docker.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true})
			return fmt.Errorf("start helper for %s: %w", volName, err)
		}

		waitResult = docker.ContainerWait(ctx, containerID, client.ContainerWaitOptions{})

		drainStderr = &bytes.Buffer{}
		drainDone = make(chan struct{})
		go func() {
			defer close(drainDone)
			_, _ = stdcopy.StdCopy(io.Discard, drainStderr, attach.Reader)
		}()

		volTW = tar.NewWriter(attach.Conn)
		currentVol = volName
		slog.Info("restoring volume", "name", volName)
		return nil
	}

	// abortVolume forcefully tears down the in-flight helper container
	// after a streaming error from our side (so we can surface a tar exit
	// code or stderr if available). Called from main loop error paths.
	abortVolume := func() string {
		if volTW == nil {
			return ""
		}
		_ = volTW.Close()
		_ = attach.CloseWrite()

		var detail string
		select {
		case res := <-waitResult.Result:
			if res.StatusCode != 0 {
				detail = fmt.Sprintf("tar exit %d: %s", res.StatusCode, strings.TrimSpace(drainStderr.String()))
			}
		case err := <-waitResult.Error:
			if err != nil {
				detail = err.Error()
			}
		}

		<-drainDone
		attach.Close()
		_, _ = docker.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true})
		volTW = nil
		return detail
	}

	restoredCount := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if detail := abortVolume(); detail != "" {
				return fmt.Errorf("read tar entry: %w (helper for %s: %s)", err, currentVol, detail)
			}
			return fmt.Errorf("read tar entry: %w", err)
		}

		// Extract volume name from first path component
		volName, relPath := splitVolumePath(hdr.Name)
		if volName == "" {
			continue
		}

		// Volume changed — finish previous, start new
		if volName != currentVol {
			if err := finishVolume(); err != nil {
				return err
			}
			if err := startVolume(volName); err != nil {
				return err
			}
			restoredCount++
		}

		// Strip volume prefix and write into volume tar stream
		hdr.Name = relPath
		if err := volTW.WriteHeader(hdr); err != nil {
			if detail := abortVolume(); detail != "" {
				return fmt.Errorf("write tar header for %s/%s: %w (helper: %s)", currentVol, hdr.Name, err, detail)
			}
			return fmt.Errorf("write tar header for %s/%s: %w", currentVol, hdr.Name, err)
		}
		if hdr.Size > 0 {
			if _, err := io.Copy(volTW, tr); err != nil {
				if detail := abortVolume(); detail != "" {
					return fmt.Errorf("write tar data for %s/%s (%d bytes, typeflag=%d): %w (helper: %s)", currentVol, hdr.Name, hdr.Size, hdr.Typeflag, err, detail)
				}
				return fmt.Errorf("write tar data for %s/%s (%d bytes, typeflag=%d): %w", currentVol, hdr.Name, hdr.Size, hdr.Typeflag, err)
			}
		}
	}

	// Finish the last volume
	if err := finishVolume(); err != nil {
		return err
	}

	fmt.Printf("Restore complete: %d volumes\n", restoredCount)
	return nil
}

// scanArchiveVolumes reads tar headers to collect unique volume names
// (top-level directories) without extracting file data.
func scanArchiveVolumes(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	zr, err := zstd.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	tr := tar.NewReader(zr)

	seen := make(map[string]bool)
	var names []string

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		volName, _ := splitVolumePath(hdr.Name)
		if volName != "" && !seen[volName] {
			seen[volName] = true
			names = append(names, volName)
		}
	}

	return names, nil
}

// splitVolumePath splits "praktor-data/some/file" into ("praktor-data", "some/file").
// Returns empty volName for invalid paths.
func splitVolumePath(name string) (volName, relPath string) {
	// Clean leading slashes/dots
	name = strings.TrimLeft(name, "./")
	if name == "" {
		return "", ""
	}

	idx := strings.IndexByte(name, '/')
	if idx < 0 {
		// Directory entry like "praktor-data" (no trailing slash was stripped)
		if strings.HasPrefix(name, volumePrefix) {
			return name, "./"
		}
		return "", ""
	}

	volName = name[:idx]
	relPath = name[idx+1:]
	if relPath == "" {
		relPath = "./"
	}

	if !strings.HasPrefix(volName, volumePrefix) {
		return "", ""
	}

	return volName, relPath
}

func listPraktorVolumes(ctx context.Context, docker *client.Client) ([]string, error) {
	resp, err := docker.VolumeList(ctx, client.VolumeListOptions{
		Filters: make(client.Filters).Add("name", volumePrefix),
	})
	if err != nil {
		return nil, err
	}

	var names []string
	for _, v := range resp.Items {
		names = append(names, v.Name)
	}
	return names, nil
}

func ensureImage(ctx context.Context, docker *client.Client, image string) error {
	_, err := docker.ImageInspect(ctx, image)
	if err == nil {
		return nil // already present
	}

	slog.Info("pulling helper image", "image", image)
	reader, err := docker.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	_, _ = io.Copy(io.Discard, reader)
	return nil
}

func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}

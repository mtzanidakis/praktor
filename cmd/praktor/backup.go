package main

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
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

	// Track current volume's streaming state
	var (
		currentVol  string
		pw          *io.PipeWriter
		volTW       *tar.Writer
		copyErr     chan error
		containerID string
	)

	finishVolume := func() error {
		if volTW == nil {
			return nil
		}
		_ = volTW.Close()
		_ = pw.Close()
		if err := <-copyErr; err != nil {
			return fmt.Errorf("copy to container for %s: %w", currentVol, err)
		}
		_, _ = docker.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true})
		return nil
	}

	startVolume := func(volName string) error {
		// Create volume if it doesn't exist
		_, err := docker.VolumeCreate(ctx, client.VolumeCreateOptions{Name: volName})
		if err != nil {
			return fmt.Errorf("create volume %s: %w", volName, err)
		}

		ctrName := fmt.Sprintf("praktor-restore-%d", time.Now().UnixNano())
		resp, err := docker.ContainerCreate(ctx, client.ContainerCreateOptions{
			Config:     &dockercontainer.Config{Image: helperImage, Entrypoint: []string{"true"}},
			HostConfig: &dockercontainer.HostConfig{Binds: []string{volName + ":/vol"}},
			Name:       ctrName,
		})
		if err != nil {
			return fmt.Errorf("create temp container: %w", err)
		}
		containerID = resp.ID

		pr, pipew := io.Pipe()
		pw = pipew
		volTW = tar.NewWriter(pw)
		copyErr = make(chan error, 1)

		go func() {
			_, err := docker.CopyToContainer(ctx, containerID, client.CopyToContainerOptions{
				DestinationPath: "/vol",
				Content:         pr,
			})
			copyErr <- err
		}()

		currentVol = volName
		slog.Info("restoring volume", "name", volName)
		return nil
	}

	restoredCount := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Clean up on error
			if volTW != nil {
				_ = volTW.Close()
				_ = pw.Close()
				<-copyErr
				_, _ = docker.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true})
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
			return fmt.Errorf("write tar header: %w", err)
		}
		if hdr.Size > 0 {
			if _, err := io.Copy(volTW, tr); err != nil {
				return fmt.Errorf("write tar data: %w", err)
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

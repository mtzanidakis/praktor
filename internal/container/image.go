package container

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	goarchive "github.com/moby/go-archive"
	"github.com/moby/moby/client"
)

func BuildAgentImage(ctx context.Context, docker *client.Client, imageName string) error {
	cwd, _ := os.Getwd()
	buildContext := cwd

	tar, err := goarchive.TarWithOptions(buildContext, &goarchive.TarOptions{})
	if err != nil {
		return fmt.Errorf("create build context: %w", err)
	}

	resp, err := docker.ImageBuild(ctx, tar, client.ImageBuildOptions{
		Tags:       []string{imageName},
		Dockerfile: "Dockerfile.agent",
		Remove:     true,
	})
	if err != nil {
		return fmt.Errorf("build image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Drain the build output
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		slog.Warn("error reading build output", "error", err)
	}

	slog.Info("agent image built", "image", imageName)
	return nil
}

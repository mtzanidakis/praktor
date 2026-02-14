package container

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/client"
	goarchive "github.com/moby/go-archive"
)

func BuildAgentImage(ctx context.Context, docker *client.Client, imageName string) error {
	cwd, _ := os.Getwd()
	buildContext := cwd

	tar, err := goarchive.TarWithOptions(buildContext, &goarchive.TarOptions{})
	if err != nil {
		return fmt.Errorf("create build context: %w", err)
	}

	resp, err := docker.ImageBuild(ctx, tar, build.ImageBuildOptions{
		Tags:       []string{imageName},
		Dockerfile: "Dockerfile.agent",
		Remove:     true,
	})
	if err != nil {
		return fmt.Errorf("build image: %w", err)
	}
	defer resp.Body.Close()

	// Drain the build output
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		slog.Warn("error reading build output", "error", err)
	}

	slog.Info("agent image built", "image", imageName)
	return nil
}

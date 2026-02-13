package container

import (
	"fmt"
	"os"
	"path/filepath"
)

type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

func buildMounts(opts AgentOpts) []string {
	cwd, _ := os.Getwd()
	var binds []string

	if opts.IsMain {
		// Main group gets project root mounted
		binds = append(binds, fmt.Sprintf("%s:%s", cwd, "/workspace/project"))
	}

	// Group-specific workspace
	groupPath := filepath.Join(cwd, "groups", opts.GroupFolder)
	binds = append(binds, fmt.Sprintf("%s:%s", groupPath, "/workspace/group"))

	// Global shared instructions (read-only)
	globalPath := filepath.Join(cwd, "groups", "global")
	binds = append(binds, fmt.Sprintf("%s:%s:ro", globalPath, "/workspace/global"))

	// Claude session data
	sessionPath := filepath.Join(cwd, "data", "sessions", opts.GroupFolder, ".claude")
	os.MkdirAll(sessionPath, 0o755)
	binds = append(binds, fmt.Sprintf("%s:%s", sessionPath, "/home/node/.claude"))

	// Extra mounts
	for _, m := range opts.Mounts {
		bind := fmt.Sprintf("%s:%s", m.Source, m.Target)
		if m.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
	}

	return binds
}

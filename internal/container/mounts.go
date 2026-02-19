package container

import (
	"fmt"
	"strings"
)

type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

func buildMounts(opts AgentOpts) []string {
	workspace := sanitizeVolumeName(opts.Workspace)
	var binds []string

	// Agent-specific workspace (named volume)
	binds = append(binds, fmt.Sprintf("praktor-wk-%s:/workspace/agent", workspace))

	// Global shared instructions (named volume, read-only)
	binds = append(binds, "praktor-global:/workspace/global:ro")

	// Claude session data (named volume)
	binds = append(binds, fmt.Sprintf("praktor-home-%s:/home/praktor", workspace))

	// Extra mounts (user-configured, kept as-is)
	for _, m := range opts.Mounts {
		bind := fmt.Sprintf("%s:%s", m.Source, m.Target)
		if m.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
	}

	return binds
}

// sanitizeVolumeName replaces characters not allowed in Docker volume names.
func sanitizeVolumeName(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

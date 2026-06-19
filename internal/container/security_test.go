package container

import (
	"testing"

	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/mtzanidakis/praktor/internal/config"
)

func balancedSecurity() config.SecurityConfig {
	return config.SecurityConfig{
		NoNewPrivileges:  true,
		DropCapabilities: true,
		AddCapabilities:  []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETUID", "SETGID"},
		PidsLimit:        1024,
		Tmpfs:            true,
	}
}

func TestApplySecurityBalancedDefaults(t *testing.T) {
	m := &Manager{cfg: config.DefaultsConfig{Security: balancedSecurity()}}
	hc := &dockercontainer.HostConfig{}
	m.applySecurity(hc, nil)

	if len(hc.SecurityOpt) != 1 || hc.SecurityOpt[0] != "no-new-privileges=true" {
		t.Errorf("SecurityOpt = %v, want [no-new-privileges=true]", hc.SecurityOpt)
	}
	if len(hc.CapDrop) != 1 || hc.CapDrop[0] != "ALL" {
		t.Errorf("CapDrop = %v, want [ALL]", hc.CapDrop)
	}
	if len(hc.CapAdd) != 5 {
		t.Errorf("CapAdd = %v, want 5 caps", hc.CapAdd)
	}
	if hc.PidsLimit == nil || *hc.PidsLimit != 1024 {
		t.Errorf("PidsLimit = %v, want 1024", hc.PidsLimit)
	}
	if hc.Tmpfs["/tmp"] != "rw,nosuid,size=512m" {
		t.Errorf("/tmp tmpfs = %q", hc.Tmpfs["/tmp"])
	}
	if hc.Tmpfs["/var/tmp"] != "rw,noexec,nosuid,size=256m" {
		t.Errorf("/var/tmp tmpfs = %q", hc.Tmpfs["/var/tmp"])
	}
	if hc.Memory != 0 || hc.NanoCPUs != 0 || hc.ReadonlyRootfs {
		t.Errorf("unexpected limits: mem=%d cpu=%d ro=%v", hc.Memory, hc.NanoCPUs, hc.ReadonlyRootfs)
	}
}

func TestApplySecurityLimits(t *testing.T) {
	m := &Manager{cfg: config.DefaultsConfig{Security: config.SecurityConfig{
		MemoryMB:       512,
		CPUs:           1.5,
		ReadonlyRootfs: true,
	}}}
	hc := &dockercontainer.HostConfig{}
	m.applySecurity(hc, nil)

	if hc.Memory != 512*1024*1024 {
		t.Errorf("Memory = %d, want %d", hc.Memory, 512*1024*1024)
	}
	if hc.NanoCPUs != 1_500_000_000 {
		t.Errorf("NanoCPUs = %d, want 1.5e9", hc.NanoCPUs)
	}
	if !hc.ReadonlyRootfs {
		t.Error("ReadonlyRootfs not set")
	}
	// Disabled toggles must not appear.
	if len(hc.SecurityOpt) != 0 || hc.CapDrop != nil || hc.Tmpfs != nil {
		t.Errorf("disabled toggles leaked: opt=%v drop=%v tmpfs=%v", hc.SecurityOpt, hc.CapDrop, hc.Tmpfs)
	}
}

func TestApplySecurityPerAgentOverride(t *testing.T) {
	// Manager default is fully hardened; the per-agent override disables it all.
	m := &Manager{cfg: config.DefaultsConfig{Security: balancedSecurity()}}
	hc := &dockercontainer.HostConfig{}
	m.applySecurity(hc, &config.SecurityConfig{}) // empty override = no hardening

	if len(hc.SecurityOpt) != 0 || hc.CapDrop != nil || hc.PidsLimit != nil || hc.Tmpfs != nil {
		t.Errorf("override should disable hardening, got opt=%v drop=%v pids=%v tmpfs=%v",
			hc.SecurityOpt, hc.CapDrop, hc.PidsLimit, hc.Tmpfs)
	}
}

func TestDefaultsHaveBalancedSecurity(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	s := cfg.Defaults.Security
	if !s.NoNewPrivileges || !s.DropCapabilities || !s.Tmpfs {
		t.Errorf("balanced toggles not set: %+v", s)
	}
	if s.PidsLimit != 1024 {
		t.Errorf("PidsLimit = %d, want 1024", s.PidsLimit)
	}
	if len(s.AddCapabilities) != 5 {
		t.Errorf("AddCapabilities = %v", s.AddCapabilities)
	}
}

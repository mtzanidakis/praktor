package agent

import "testing"

func TestCheckCommandAllowed(t *testing.T) {
	cases := []struct {
		name  string
		tools []string
		want  bool
	}{
		{"unrestricted agent", nil, true},
		{"empty list", []string{}, true},
		{"list with Bash", []string{"Read", "Bash", "WebSearch"}, true},
		{"list without Bash", []string{"Read", "WebSearch", "mcp__praktor-tasks"}, false},
		{"lookalike tool", []string{"BashOutput"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CheckCommandAllowed(c.tools); got != c.want {
				t.Errorf("CheckCommandAllowed(%v) = %v, want %v", c.tools, got, c.want)
			}
		})
	}
}

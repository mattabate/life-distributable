package threads

import (
	"strings"
	"testing"
)

func TestSessionPreflightSelectsSessionCLI(t *testing.T) {
	owner := &Manager{ClaudeBin: "/owner/claude"}
	if name, args := owner.sessionCLICommand("--version"); name != "/owner/claude" || len(args) != 1 || args[0] != "--version" {
		t.Fatalf("owner command %q %q", name, args)
	}
	m := &Manager{
		ClaudeBin: "/owner/claude",
		RunAs: RunAs{
			User:      "synthetic-review",
			ClaudeBin: "/session/claude",
		},
	}
	want := []string{"-n", "-H", "-u", "synthetic-review", "/bin/sh", "-c"}
	for _, args := range [][]string{{"--version"}, {"-p", "--model", "synthetic-model", "--output-format", "text", "--max-turns", "1", "--tools", ""}} {
		name, got := m.sessionCLICommand(args...)
		if name != "/usr/bin/sudo" || len(got) != len(want)+1 {
			t.Fatalf("command %q %q", name, got)
		}
		for i, part := range want {
			if got[i] != part {
				t.Errorf("command arg %d = %q, want %q", i, got[i], part)
			}
		}
		if !strings.Contains(got[len(got)-1], "'/session/claude'") || strings.Contains(got[len(got)-1], "/owner/claude") {
			t.Errorf("command uses wrong CLI: %q", got[len(got)-1])
		}
		if !strings.Contains(got[len(got)-1], `$(cat "$HOME/.claude-token")`) {
			t.Errorf("session token environment missing: %q", got[len(got)-1])
		}
		for _, arg := range args {
			if !strings.Contains(got[len(got)-1], shellQuote(arg)) {
				t.Errorf("command missing argument %q: %q", arg, got[len(got)-1])
			}
		}
	}
}

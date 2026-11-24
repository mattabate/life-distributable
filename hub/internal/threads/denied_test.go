package threads

import (
	"strings"
	"testing"
)

// #265: a session whose lifectl call its own grant refused gets one error
// card naming the command, raised once; other refusals raise nothing.
func TestRefusedLifectlRaisesOneErrorCard(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "staff", "", "", nil)
	other := `{"type":"result","result":"ok","permission_denials":[{"tool_name":"Bash","tool_input":{"command":"rm -rf data"}}]}`
	m.grantRefused(th.ID, other)
	if asks, _ := m.ListAsks("all", th.ID, 10); len(asks) != 0 {
		t.Fatalf("a refused non-lifectl call raised %d card(s)", len(asks))
	}
	line := `{"type":"result","result":"ok","permission_denials":[{"tool_name":"Bash","tool_input":{"command":"/repo/ops/bin/lifectl personas"}}]}`
	m.grantRefused(th.ID, line)
	m.grantRefused(th.ID, line)
	asks, _ := m.ListAsks("all", th.ID, 10)
	if len(asks) != 1 || asks[0].Kind != "error" || asks[0].Title != grantRefusedTitle {
		t.Fatalf("want one error card, got %+v", asks)
	}
	if !strings.Contains(asks[0].Detail, "lifectl personas") || !strings.Contains(asks[0].Detail, "not a tool to approve") {
		t.Fatalf("detail: %q", asks[0].Detail)
	}
}

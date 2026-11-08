package server

import "testing"

// The install's shell line quotes every word (#156): a path with a space,
// a ; or a $ stays one argument.
func TestInstallLineQuotesEveryWord(t *testing.T) {
	got := installLine([]string{"bash", "/My Projects/life/ops/mac-apply.sh", "3060", "x; touch /tmp/x $(id)"}, "/My Projects/life/ops/logs/app-install.log")
	want := `'bash' '/My Projects/life/ops/mac-apply.sh' '3060' 'x; touch /tmp/x $(id)' > '/My Projects/life/ops/logs/app-install.log' 2>&1; echo "exit=$?" >> '/My Projects/life/ops/logs/app-install.log'`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := installLine([]string{"it's"}, "/l"); got != `'it'\''s' > '/l' 2>&1; echo "exit=$?" >> '/l'` {
		t.Fatalf("quote in a word: %s", got)
	}
}

package actions

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTruncateCutsOnAWholeCharacter: the session-send title cap (120) and the
// approval push detail (300) must not leave invalid UTF-8 in the action row
// or the push line when a curly quote or an em dash falls across the cap.
func TestTruncateCutsOnAWholeCharacter(t *testing.T) {
	title := "Send to the session “" + strings.Repeat("a", 96) + "’s shopping list”"
	got := truncate(title, 120)
	if !utf8.ValidString(got) {
		t.Fatalf("truncate split a character: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("want a cut ending in …, got %q", got)
	}
	if s := "short — fits"; truncate(s, 120) != s {
		t.Fatalf("a string under the cap changed: %q", truncate(s, 120))
	}
}

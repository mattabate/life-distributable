package calendar

import (
	"strings"
	"testing"

	"life/hub/internal/store"
)

// A step's title is cleaned like an item's: one line, whole characters, a
// title of only control characters dropped like a blank one.
func TestStepTitlesAreOneLineAndWholeCharacters(t *testing.T) {
	long := strings.Repeat("a", 199) + "é and more" // é is bytes 199-200
	got := cleanSteps([]store.Step{{Title: "Wash\nyour\tface\r\n"}, {Title: long}, {Title: "\n\t"}})
	if len(got) != 2 || got[0] != "Wash your face" || got[1] != strings.Repeat("a", 199) {
		t.Fatalf("steps = %q", got)
	}
}

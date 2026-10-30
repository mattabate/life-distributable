package threads

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// A card title is stored on one line and in whole characters: a byte cut at
// 200 halved an accent or an emoji into invalid UTF-8; a newline made the
// title two lines. Raise and reword both clean it.
func TestAskTitleIsOneLineAndWholeCharacters(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "titles", "", "", nil)
	a, err := m.AddAsk(th.ID, "", " Book the vet\nfor\tthe cat\r\n", "", "read", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Title != "Book the vet for the cat" {
		t.Fatalf("add stored %q", a.Title)
	}
	long := strings.Repeat("a", 199) + "é and more" // é is bytes 199-200
	r, err := m.RewordAsk(a.ID, long, "", "", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("a", 199); r.Title != want {
		t.Fatalf("reword stored %q, want 199 a's", r.Title)
	}
	if s := cardTitle(strings.Repeat("🐈", 60)); !utf8.ValidString(s) || len(s) != 200 {
		t.Fatalf("a cap through four-byte characters is not whole characters: %d bytes", len(s))
	}
	if _, err := m.AddAsk(th.ID, "", "\n\t", "", "read", ""); err == nil {
		t.Fatal("a title of only control characters accepted")
	}
}

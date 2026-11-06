package threads

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// A paste of 100+ lines reaches the session as a file in the intake in-box:
// the prompt carries its two ends (the owner's words follow the paste) and
// the path, the chat keeps the message whole.
func TestLongPasteGoesAsAFile(t *testing.T) {
	m, _, _ := setup(t)
	var saved, savedAs string
	m.SavePaste = func(name, text string) (string, error) {
		saved, savedAs = text, name
		return "/data/imports/intake/" + name, nil
	}
	var lines []string
	for i := 1; i <= 150; i++ {
		lines = append(lines, fmt.Sprintf("row %03d", i))
	}
	msg := strings.Join(lines, "\n") + "\n\nSo put these into the ledger."
	th, err := m.Create("", "life", "", msg, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if saved != msg || !strings.HasPrefix(savedAs, "paste ") || !strings.HasSuffix(savedAs, ".txt") {
		t.Fatalf("saved %q as %q", saved[:min(40, len(saved))], savedAs)
	}
	rows, _ := m.db.Query(`SELECT out_file FROM thread_runs WHERE thread_id=?`, th.ID)
	var out string
	for rows.Next() {
		rows.Scan(&out)
	}
	rows.Close()
	p, _ := os.ReadFile(strings.TrimSuffix(out, ".out") + ".prompt")
	for _, want := range []string{"152 lines", "/data/imports/intake/paste ", "Read it in full", "row 001", "row 015", "the middle is in the file", "So put these into the ledger."} {
		if !strings.Contains(string(p), want) {
			t.Fatalf("prompt missing %q: %s", want, p)
		}
	}
	if strings.Contains(string(p), "row 075") {
		t.Fatalf("the middle should be in the file only: %s", p)
	}
	msgs, _ := m.Messages(th.ID, 10)
	if len(msgs) == 0 || msgs[0].Text != msg {
		t.Fatalf("the chat keeps the whole message: %+v", msgs)
	}

	// A short message goes in whole, nothing saved.
	saved = ""
	if _, err := m.Create("", "life", "", "just this", "", "", nil); err != nil || saved != "" {
		t.Fatalf("short message saved %q (%v)", saved, err)
	}
}

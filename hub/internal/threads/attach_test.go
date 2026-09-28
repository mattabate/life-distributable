package threads

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestAttachments(t *testing.T) {
	m, _, _ := setup(t)
	// No resolver → attachments rejected, nothing recorded.
	if _, err := m.Create("", "life", "", "what is this?", "", "", []string{"sha256/ab/abc.jpg"}); err == nil {
		t.Fatal("expected error without BlobPath")
	}
	m.BlobPath = func(ref string) (string, error) {
		if ref != "sha256/ab/abc.jpg" {
			return "", errors.New("no such blob")
		}
		return "/data/blobs/" + ref, nil
	}
	if _, err := m.Create("", "life", "", "x", "", "", []string{"nope"}); err == nil || !strings.Contains(err.Error(), "no such blob") {
		t.Fatalf("bad ref should fail: %v", err)
	}
	// Photo-only first message: allowed, title synthesized, prompt lists the path.
	th, err := m.Create("", "life", "", "", "", "", []string{"sha256/ab/abc.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(th.Title, "Photo ") {
		t.Fatalf("title %q", th.Title)
	}
	rows, _ := m.db.Query(`SELECT out_file FROM thread_runs WHERE thread_id=?`, th.ID)
	var out string
	for rows.Next() {
		rows.Scan(&out)
	}
	rows.Close()
	p, _ := os.ReadFile(strings.TrimSuffix(out, ".out") + ".prompt")
	if !strings.Contains(string(p), "/data/blobs/sha256/ab/abc.jpg") || !strings.Contains(string(p), "Read tool") {
		t.Fatalf("prompt missing attachment: %s", p)
	}
	msgs, _ := m.Messages(th.ID, 10)
	if len(msgs) != 1 || len(msgs[0].Attachments) != 1 || msgs[0].Attachments[0] != "sha256/ab/abc.jpg" {
		t.Fatalf("%+v", msgs)
	}
	complete(t, m, th.ID, "a receipt")
	if err := m.SendAttached(th.ID, "", nil); err == nil {
		t.Fatal("empty send must fail")
	}
	msgs, _ = m.Messages(th.ID, 10)
	if msgs[1].Attachments == nil || len(msgs[1].Attachments) != 0 {
		t.Fatalf("claude message should have empty (non-null) attachments: %+v", msgs[1])
	}
}

// A PDF shared from the phone is a file, not a picture: the
// prompt says to Read it page by page, the placeholder title says File.
func TestAttachedDocument(t *testing.T) {
	m, _, _ := setup(t)
	m.BlobPath = func(ref string) (string, error) { return "/data/blobs/" + ref, nil }
	th, err := m.Create("", "life", "", "", "", "", []string{"sha256/cd/cde.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(th.Title, "File ") {
		t.Fatalf("title %q", th.Title)
	}
	rows, _ := m.db.Query(`SELECT out_file FROM thread_runs WHERE thread_id=?`, th.ID)
	var out string
	for rows.Next() {
		rows.Scan(&out)
	}
	rows.Close()
	p, _ := os.ReadFile(strings.TrimSuffix(out, ".out") + ".prompt")
	for _, want := range []string{"/data/blobs/sha256/cd/cde.pdf", "1 file(s)", "pages=", "app/upload", "sent a file with no message"} {
		if !strings.Contains(string(p), want) {
			t.Fatalf("prompt missing %q: %s", want, p)
		}
	}
	if strings.Contains(string(p), "image(s)") {
		t.Fatalf("a lone PDF is not an image: %s", p)
	}
}

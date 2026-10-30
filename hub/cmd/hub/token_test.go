package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets", "hub.token")

	// Missing: a new 48-hex token, 0600, read back the same.
	tok, err := loadOrCreateToken(path)
	if err != nil || len(tok) != 48 {
		t.Fatalf("new token %q, %v", tok, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode: %v %v", fi, err)
	}
	if again, err := loadOrCreateToken(path); err != nil || again != tok {
		t.Fatalf("reread %q, %v; want %q", again, err, tok)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "secrets", ".hub.token-*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}

	// Padded at either end: read as its clients read it (TrimSpace).
	good := strings.Repeat("a", 40)
	os.WriteFile(path, []byte(" "+good+" \t\r\n"), 0o600)
	if got, err := loadOrCreateToken(path); err != nil || got != good {
		t.Fatalf("padded token = %q, %v", got, err)
	}

	// There but not a token: refused, and the file is left as it was.
	for _, body := range []string{"", "\n", "short\n", strings.Repeat("b", 31) + "\n", strings.Repeat("c", 20) + " " + strings.Repeat("d", 20)} {
		os.WriteFile(path, []byte(body), 0o600)
		if got, err := loadOrCreateToken(path); err == nil {
			t.Fatalf("%q accepted as %q", body, got)
		}
		if b, _ := os.ReadFile(path); string(b) != body {
			t.Fatalf("%q was overwritten with %q", body, b)
		}
	}
}

package actions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeciderIsUnarmedUntilASecretIsSet(t *testing.T) {
	d := NewDecider(filepath.Join(t.TempDir(), "decider.hash"))
	if d.Armed() {
		t.Fatal("a hub with no hash file must behave exactly as it did before")
	}
	if d.Verify("anything") {
		t.Fatal("an unarmed decider must never verify a code")
	}
}

func TestDeciderVerifiesOnlyTheRealSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decider.hash")
	d := NewDecider(path)
	secret, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Arm(secret); err != nil {
		t.Fatal(err)
	}
	if !d.Armed() {
		t.Fatal("armed after Arm")
	}
	if !d.Verify(secret) || !d.Verify("  "+secret+"\n") {
		t.Fatal("the real secret must verify, whitespace and all — the owner pastes it by hand")
	}
	for _, bad := range []string{"", "nope", secret + "X", strings.ToLower(secret)} {
		if d.Verify(bad) {
			t.Fatalf("verified a wrong code: %q", bad)
		}
	}

	// The file on disk is the hash, never the secret: a session can read it.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatal("the secret itself must never touch disk")
	}
	if len(strings.TrimSpace(string(b))) != 64 {
		t.Fatalf("want a sha256 hex digest, got %q", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("hash file mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestNewSecretIsTypeableAndUnguessable(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		s, err := NewSecret()
		if err != nil {
			t.Fatal(err)
		}
		if seen[s] {
			t.Fatal("NewSecret repeated itself")
		}
		seen[s] = true
		if len(s) != 23 || strings.Count(s, "-") != 3 {
			t.Fatalf("want XXXXX-XXXXX-XXXXX-XXXXX, got %q", s)
		}
		if strings.ContainsAny(s, "ILOU") {
			t.Fatalf("ambiguous letters make a hand-typed code worse: %q", s)
		}
	}
	if err := NewDecider(filepath.Join(t.TempDir(), "h")).Arm("short"); err == nil {
		t.Fatal("a short secret must be refused")
	}
}

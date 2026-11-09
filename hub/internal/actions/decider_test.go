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

// Armed once, armed for good: a session that empties or deletes the hash
// file gets refusals, not the hub token's old free pass.
func TestDeciderFailsClosedOnceArmed(t *testing.T) {
	q, _, _ := setup(t)
	path := filepath.Join(t.TempDir(), "decider.hash")
	if q.Decider(path).Armed() {
		t.Fatal("never armed = unarmed")
	}
	secret, _ := NewSecret()
	q.Decider(path).Arm(secret)
	if !q.Decider(path).Armed() {
		t.Fatal("armed after Arm")
	}
	for _, tamper := range []func(){func() { os.WriteFile(path, nil, 0o600) }, func() { os.Remove(path) }} {
		tamper()
		d := q.Decider(path)
		if !d.Armed() || d.Verify(secret) || d.Verify("") {
			t.Fatal("a tampered hash file must refuse every decision")
		}
	}
	q.Decider(path).Arm(secret)
	if !q.Decider(path).Verify(secret) {
		t.Fatal("decider-set.sh's new hash works again")
	}
}

// A session that rewrites the hash file with the hash of a code it chose
// (no session_user: it runs as the owner and can write the file) is refused,
// and the refusal says why (#182). decider-set.sh moves the pin, so a real
// rotation works.
func TestDeciderRefusesAHashRewrittenAfterArming(t *testing.T) {
	q, _, _ := setup(t)
	path := filepath.Join(t.TempDir(), "decider.hash")
	owner, _ := NewSecret()
	q.Decider(path).Arm(owner)
	mine, _ := NewSecret()
	NewDecider(path).Arm(mine) // a plain file write, as a session's Write would be
	d := q.Decider(path)
	if !d.Tampered() || d.Verify(mine) || d.Verify(owner) {
		t.Fatal("a hash file rewritten outside decider-set.sh must refuse every code")
	}
	q.db.SetSetting(pinKey, d.hash()) // what decider-set.sh does before replacing the file
	if d.Tampered() || !d.Verify(mine) {
		t.Fatal("after decider-set.sh moves the pin the new code works")
	}
	// A hub armed before the pin existed pins the file it sees at start.
	q2, _, _ := setup(t)
	p2 := filepath.Join(t.TempDir(), "decider.hash")
	NewDecider(p2).Arm(owner)
	if !q2.Decider(p2).Armed() || q2.db.Setting(pinKey) == "" || !q2.Decider(p2).Verify(owner) {
		t.Fatal("an armed hub with no pin pins the file it sees")
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

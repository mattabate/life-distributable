package actions

// The second credential.
//
// Every gated action needs the owner's approval, but without this "the
// owner" means "whoever holds the hub token" — and every session holds the
// hub token, because it is in ops/secrets/hub.token, in the prompt footer, and
// in lifectl's environment. A session that swallowed an injected instruction
// could therefore propose a money/delete/commit action and approve its own
// proposal in the next tool call.
//
// The decider secret closes that: approve/deny also require a value that
// exists only on the owner's phone (Keychain) and in their browser
// (localStorage).
// The hub never stores the secret — only its SHA-256, in a file beside
// hub.token. A session can read that hash and learn nothing: the secret is
// 100 bits of randomness, so there is nothing to guess.
//
// It is armed by ops/decider-set.sh, which refuses to run without a terminal,
// so no session can mint one either. While no hash file exists, approve/deny
// behave exactly as they always did.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"
)

type Decider struct{ path string }

func NewDecider(hashPath string) *Decider { return &Decider{path: hashPath} }

// Armed reports whether a decider secret has been set. Unarmed = the old
// behaviour, so the hub keeps working until the owner runs
// ops/decider-set.sh.
func (d *Decider) Armed() bool { return d.hash() != "" }

func (d *Decider) hash() string {
	if d == nil || d.path == "" {
		return ""
	}
	b, err := os.ReadFile(d.path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SetAt is when the code the hub holds was made: the hash file's write time
// (decider-set.sh replaces the file, so it is the last rotation). Zero when
// unarmed. It is the one fact about the current code that is not a secret,
// and it is what a refused code gets compared with, so a refusal can say
// which side is older with a date on each.
func (d *Decider) SetAt() time.Time {
	if d == nil || d.path == "" {
		return time.Time{}
	}
	fi, err := os.Stat(d.path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// Verify is constant-time and never logs the secret.
func (d *Decider) Verify(secret string) bool {
	want := d.hash()
	if want == "" || secret == "" {
		return false
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(want)) == 1
}

// Arm stores the hash of secret (mode 0600). The secret itself is the
// caller's to hand to the owner and forget.
func (d *Decider) Arm(secret string) error {
	secret = strings.TrimSpace(secret)
	if len(secret) < 16 {
		return errors.New("decider secret must be at least 16 characters")
	}
	sum := sha256.Sum256([]byte(secret))
	return os.WriteFile(d.path, []byte(hex.EncodeToString(sum[:])+"\n"), 0o600)
}

// Refused records an approve/deny that arrived without a valid decider code.
// It belongs in the action's own audit trail: a session trying to approve its
// own proposal is the exact event this whole mechanism exists to catch.
func (q *Queue) Refused(id, via string) {
	q.event(id, "refused", via, "approve/deny without a valid decider code")
}

// NewSecret returns 100 bits of randomness in Crockford-ish base32, grouped
// for typing by hand: XXXXX-XXXXX-XXXXX-XXXXX.
func NewSecret() (string, error) {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ" // no I, L, O, U
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	var sb strings.Builder
	for i, c := range b {
		if i > 0 && i%5 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(alphabet[int(c)%len(alphabet)])
	}
	return sb.String(), nil
}

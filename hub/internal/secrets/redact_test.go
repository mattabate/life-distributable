package secrets

import (
	"strings"
	"testing"
)

// The fixtures are fake and joined at run time, so no line here has a
// credential's shape for a secret scanner to flag.
func TestRedact(t *testing.T) {
	fill := strings.Repeat("a1B2", 12)
	for _, c := range []struct{ in, gone, want string }{
		{"token " + "github" + "_pat_" + fill + " ok", fill, "token [redacted GitHub token] ok"},
		{"gh" + "p_" + fill, fill, "[redacted GitHub token]"},
		{"key=" + "AI" + "za" + fill[:35] + ".", fill[:35], "key=[redacted Google API key]."},
		{"https://" + "user0123:" + fill + "@bank.example.com/access", fill, "https://[redacted login]@bank.example.com/access"},
		{"s" + "1.some-session-0000aaaa." + strings.Repeat("0f", 32), "0f0f", "[redacted session token]"},
		{"-----BEGIN " + "PRIVATE KEY-----\nMIIabc\n-----END " + "PRIVATE KEY----- after", "MIIabc", "[redacted private key] after"},
	} {
		got := Redact(c.in)
		if got != c.want || strings.Contains(got, c.gone) || !HasShape(c.in) {
			t.Errorf("Redact(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Ordinary text is left alone.
	for _, s := range []string{
		"see https://github.com/example/project/issues/204 and mail someone@example.com",
		"https://my-mac.example.ts.net:8443/ota/x/manifest.plist",
		"ask-0000abcd at 17:24: the task-id is 0123456789abcdef0",
		"git@github.com:example/project.git",
	} {
		if Redact(s) != s || HasShape(s) {
			t.Errorf("Redact changed %q to %q", s, Redact(s))
		}
	}
}

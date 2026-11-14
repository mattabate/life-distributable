// Package secrets holds what the hub knows about credential shapes.
package secrets

import "regexp"

// Redact replaces what has the shape of a live credential with a label. A key
// pasted into a chat, or one a tool printed, used to be stored whole in
// thread_messages and thread_events, where any session can read it back with
// `ops/db.sh`. The shapes are vendor key formats that cannot be ordinary
// words; the label keeps the row readable.
func Redact(s string) string {
	for _, sh := range shapes {
		s = sh.re.ReplaceAllString(s, sh.to)
	}
	return s
}

// HasShape reports whether Redact would change s.
func HasShape(s string) bool {
	for _, sh := range shapes {
		if sh.re.MatchString(s) {
			return true
		}
	}
	return false
}

var shapes = []struct {
	re *regexp.Regexp
	to string
}{
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(-----END [A-Z ]*PRIVATE KEY-----|\z)`), "[redacted private key]"},
	{regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{30,}`), "[redacted GitHub token]"},
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}`), "[redacted GitHub token]"},
	{regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{24,}`), "[redacted Anthropic key]"},
	{regexp.MustCompile(`\bsk-(proj-)?[A-Za-z0-9_-]{32,}`), "[redacted API key]"},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`), "[redacted Google API key]"},
	{regexp.MustCompile(`\bya29\.[0-9A-Za-z_-]{20,}`), "[redacted Google token]"},
	{regexp.MustCompile(`\b1//0[0-9A-Za-z_-]{30,}`), "[redacted Google refresh token]"},
	{regexp.MustCompile(`\bGOCSPX-[0-9A-Za-z_-]{20,}`), "[redacted Google client secret]"},
	{regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`), "[redacted AWS key]"},
	{regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]{10,}`), "[redacted Slack token]"},
	{regexp.MustCompile(`\btskey-[a-z]+-[0-9A-Za-z-]{10,}`), "[redacted Tailscale key]"},
	{regexp.MustCompile(`\bK00[0-9A-Za-z/+]{28}\b`), "[redacted B2 key]"},
	{regexp.MustCompile(`\bs1\.[A-Za-z0-9_-]{4,}\.[0-9a-f]{32,}`), "[redacted session token]"},
	// user:password@ inside a URL.
	{regexp.MustCompile(`(https?://)[^\s/:@"'<>]{1,80}:[^\s/:@"'<>]{8,}@`), "${1}[redacted login]@"},
}

package notify

import (
	"strings"
	"testing"
)

// Only a clean, sayable line replaces the label form of a push.
func TestCleanVoice(t *testing.T) {
	line, err := cleanVoice("\"Hey Alex, I fixed the sessions page.\n Now there's a table.\"")
	if err != nil || line != "Hey Alex, I fixed the sessions page. Now there's a table." {
		t.Fatalf("got %q %v", line, err)
	}
	for _, bad := range []string{"  ", "**"} {
		if line, err := cleanVoice(bad); err == nil {
			t.Fatalf("%.20q must be refused, got %q", bad, line)
		}
	}
	// A long line is NOT refused (a refused one used to go out as "To read.
	// <cut title>. <session name>"). Length is apns.push's business: truncate
	// at voiceMax, never the label form.
	long := strings.Repeat("word ", 200)
	if line, err := cleanVoice(long); err != nil || !strings.HasPrefix(line, "word word") {
		t.Fatalf("a long line must pass through: %q %v", line, err)
	}
}

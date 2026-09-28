package spend

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Live diagnostic, skipped by default: dumps the raw /usage payload and the
// joined Quota so a session can answer "where am I on the limits right now"
// without going through the hub's TLS + token. It talks to Anthropic and reads
// the Keychain, so it must never run in `make check`.
//
//	LIFE_LIVE_QUOTA=1 go test ./internal/spend -run TestLiveQuota -v
func TestLiveQuota(t *testing.T) {
	if os.Getenv("LIFE_LIVE_QUOTA") != "1" {
		t.Skip("set LIFE_LIVE_QUOTA=1 to hit the live usage endpoint")
	}
	q := NewQuotaFetcher()
	raw, err := q.raw(context.Background())
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	for k, v := range raw {
		t.Logf("RAW %s = %s", k, string(v))
	}
	us, err := ParseAll(os.ExpandEnv("$HOME/.claude/projects"))
	if err != nil {
		t.Logf("parse transcripts: %v", err)
	}
	b, _ := json.MarshalIndent(q.Quota(context.Background(), us, time.Now()), "", "  ")
	t.Logf("QUOTA %s", string(b))
}

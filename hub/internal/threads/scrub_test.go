package threads

import (
	"strings"
	"testing"
)

// A key stored before the hub redacted on the way in is scrubbed once at
// start; a row without one is not touched.
func TestScrubStoredSecrets(t *testing.T) {
	m, _, _ := setup(t)
	key := "github" + "_pat_" + strings.Repeat("a1B2", 12)
	m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text) VALUES ('t','2026-01-01T00:00:00Z','owner','message',?)`, "here it is "+key)
	m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text) VALUES ('t','2026-01-01T00:00:01Z','owner','message','see https://example.com/a:b and x@y.z')`)
	m.db.Exec(`INSERT INTO thread_events (thread_id, run_id, ts, kind, body) VALUES ('t','r','2026-01-01T00:00:00Z','tool_result',?)`, "out: "+key)
	m.db.Exec(`DELETE FROM settings WHERE key=?`, scrubKey)
	m.scrubStoredSecrets()
	var left, plain int
	m.db.QueryRow(`SELECT (SELECT COUNT(*) FROM thread_messages WHERE text LIKE '%'||?||'%') + (SELECT COUNT(*) FROM thread_events WHERE body LIKE '%'||?||'%')`, key, key).Scan(&left)
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_messages WHERE text='see https://example.com/a:b and x@y.z'`).Scan(&plain)
	if left != 0 || plain != 1 {
		t.Fatalf("%d rows still hold the key, %d plain rows kept", left, plain)
	}
	if m.db.Setting(scrubKey) == "" {
		t.Fatal("the scrub did not record that it ran")
	}
}

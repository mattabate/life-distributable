package threads

import (
	"fmt"
	"log"
	"time"

	"life/hub/internal/secrets"
)

// scrubKey: bump the suffix when secrets.Redact learns a shape, and the next
// start scrubs the stored rows for it once.
const scrubKey = "secrets_scrubbed:1"

// scrubStoredSecrets runs secrets.Redact once over the chat already stored:
// the keys pasted or printed before the hub redacted on the way in. The SQL
// only narrows to rows that could hold one; Redact decides. Backups made
// before still hold the old text, so a key found here should be rotated, not
// just hidden.
func (m *Manager) scrubStoredSecrets() {
	if m.db.Setting(scrubKey) != "" {
		return
	}
	const maybe = ` GLOB '*github_pat_*' OR %[1]s GLOB '*gh[pousr]_[A-Za-z0-9][A-Za-z0-9]*' OR %[1]s GLOB '*sk-[A-Za-z0-9_-][A-Za-z0-9_-][A-Za-z0-9_-]*' OR %[1]s GLOB '*AIza*' OR %[1]s GLOB '*ya29.*' OR %[1]s GLOB '*1//0*' OR %[1]s GLOB '*GOCSPX-*' OR %[1]s GLOB '*AKIA*' OR %[1]s GLOB '*ASIA*' OR %[1]s GLOB '*xox[baprs]-*' OR %[1]s GLOB '*tskey-*' OR %[1]s GLOB '*K00*' OR %[1]s GLOB '*s1.*' OR %[1]s GLOB '*PRIVATE KEY-----*' OR %[1]s GLOB '*://*:*@*'`
	n := 0
	for _, t := range []struct{ table, col string }{
		{"thread_messages", "text"}, {"thread_events", "body"}, {"thread_events", "title"}, {"thread_events", "summary"}, {"prompts", "text"},
	} {
		where := t.col + fmt.Sprintf(maybe, t.col)
		rows, err := m.db.Query(`SELECT rowid, ` + t.col + ` FROM ` + t.table + ` WHERE ` + where)
		if err != nil {
			log.Printf("threads: scrub %s.%s: %v", t.table, t.col, err)
			return
		}
		fix := map[int64]string{}
		for rows.Next() {
			var id int64
			var s string
			if rows.Scan(&id, &s) == nil && secrets.HasShape(s) {
				fix[id] = secrets.Redact(s)
			}
		}
		rows.Close()
		for id, s := range fix {
			if _, err := m.db.Exec(`UPDATE `+t.table+` SET `+t.col+`=? WHERE rowid=?`, s, id); err != nil {
				log.Printf("threads: scrub %s.%s: %v", t.table, t.col, err)
				return
			}
		}
		n += len(fix)
	}
	log.Printf("threads: %d stored rows held a credential shape; redacted", n)
	m.db.SetSetting(scrubKey, ts(time.Now()))
}

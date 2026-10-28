package threads

import (
	"os"
	"path/filepath"
	"testing"
)

// A helper's steps come from its own transcript beside the session's, in
// the shape of Events, paged by position.
func TestTaskEventsReadTheHelperTranscript(t *testing.T) {
	m, _, _ := setup(t)
	m.Transcripts = t.TempDir()
	th, _ := m.Create("", "life", "", "Fan out.", "", "", nil)
	m.db.Exec(`UPDATE threads SET claude_session_id='sess-9' WHERE id=?`, th.ID)
	dir := filepath.Join(m.Transcripts, "-proj", "sess-9", "subagents")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "agent-ab95.jsonl"), []byte(`{"type":"user","message":{"content":"Read the paper."},"timestamp":"2026-10-28T03:00:00Z"}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"Start with the abstract."},{"type":"tool_use","id":"tu1","name":"Read","input":{"file_path":"/tmp/p.md"}}]},"timestamp":"2026-10-28T03:00:01Z"}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"abstract"}]},"timestamp":"2026-10-28T03:00:02Z"}
`), 0o644)
	evs, err := m.TaskEvents(th.ID, "ab95", 0, 500)
	if err != nil || len(evs) != 3 || evs[0].Kind != "thinking" || evs[1].Kind != "tool_use" || evs[2].Kind != "tool_result" || evs[2].ID != 3 || evs[2].TS.IsZero() {
		t.Fatalf("%v %+v", err, evs)
	}
	if evs, _ = m.TaskEvents(th.ID, "agent-ab95", 2, 500); len(evs) != 1 || evs[0].ID != 3 {
		t.Fatalf("since: %+v", evs)
	}
	for _, bad := range []string{"nope", "../ab95", ""} {
		if _, err := m.TaskEvents(th.ID, bad, 0, 500); err != ErrNoTask {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}

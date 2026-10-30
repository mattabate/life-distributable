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

// A sync helper's messages stream inline with the session's (they carry
// parent_tool_use_id). They are the helper row's steps, read from its own
// transcript above, never the session's: in its feed the helper's first
// result would close the open Agent call and every helper call would count
// twice. Their usage still counts.
func TestHelperLinesStayOutOfTheSessionFeed(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Fan out.", "", "", nil)
	out := runOut(t, m, th.ID)
	stream := `{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg_1","content":[{"type":"tool_use","id":"tu_a","name":"Agent","input":{"description":"Read the paper","prompt":"go"}}]}}
{"type":"assistant","parent_tool_use_id":"tu_a","message":{"id":"msg_2","usage":{"input_tokens":7,"output_tokens":3},"content":[{"type":"tool_use","id":"tu_r","name":"Read","input":{"file_path":"/tmp/p.md"}}]}}
{"type":"user","parent_tool_use_id":"tu_a","message":{"content":[{"type":"tool_result","tool_use_id":"tu_r","content":"abstract"}]}}
{"type":"user","parent_tool_use_id":null,"message":{"content":[{"type":"tool_result","tool_use_id":"tu_a","content":"summary"}]}}
`
	os.WriteFile(out, []byte(stream), 0o644)
	m.Poll()
	evs, _ := m.Events(th.ID, 0, 0, 50)
	if len(evs) != 2 || evs[0].Kind != "tool_use" || evs[1].Kind != "tool_result" || evs[1].Body != "summary" {
		t.Fatalf("session feed: %+v", evs)
	}
	if got, _ := m.Get(th.ID); got.Total != 10 {
		t.Fatalf("helper usage dropped: %+v", got.Tokens)
	}
}

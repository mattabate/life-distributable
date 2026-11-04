package threads

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A background shell's log is read from the CLI's task file: its lines, its
// newest line and a progress count, on the thread itself.
func TestShellTaskLogAndProgress(t *testing.T) {
	m, _, _ := setup(t)
	m.Transcripts, m.TaskTmp = t.TempDir(), t.TempDir()
	th, _ := m.Create("", "life", "", "Post them.", "", "", nil)
	m.db.Exec(`UPDATE threads SET claude_session_id='sess-7' WHERE id=?`, th.ID)
	os.MkdirAll(filepath.Join(m.Transcripts, "-Users-x-wt"), 0o755)
	os.WriteFile(filepath.Join(m.Transcripts, "-Users-x-wt", "sess-7.jsonl"), nil, 0o644)
	dir := filepath.Join(m.TaskTmp, "claude-"+strconv.Itoa(os.Getuid()), "-Users-x-wt", "sess-7", "tasks")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "bq1.output"), []byte("start\n\n\x1b[32mposted a/b\x1b[0m\nfiles  10%|#  | 1/10\rfiles  30%|### | 3/10\n"), 0o644)
	lines, cut, err := m.TaskOutput(th.ID, "bq1")
	if err != nil || cut || len(lines) != 3 || lines[1] != "posted a/b" || lines[2] != "files  30%|### | 3/10" {
		t.Fatalf("%v %v %q", err, cut, lines)
	}
	if _, _, err := m.TaskOutput(th.ID, "../bq1"); err != ErrNoTask {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 3, 22, 0, 0, 0, time.UTC)
	got := Thread{ID: th.ID, TurnTasks: []TurnTask{{ID: "bq1", Type: "local_bash", Description: "Post the issues", StartedAt: &at}}}
	m.fillTasks(&got)
	if k := got.TurnTasks[0]; k.Doing != "files  30%|### | 3/10" || k.Done != 3 || k.Total != 10 {
		t.Fatalf("%+v", k)
	}
}

func TestProgressOf(t *testing.T) {
	for _, c := range []struct {
		line        string
		done, total int
	}{
		{"posted 12/40 issues", 12, 40},
		{"row 7 of 9", 7, 9},
		{"downloading 42.6%", 43, 100},
		{"2026/11/03 22:10 posted", 0, 0},
		{"ok", 0, 0},
	} {
		if d, n := progressOf(c.line); d != c.done || n != c.total {
			t.Errorf("%q: %d/%d", c.line, d, n)
		}
	}
}

// A task keeps the start it had in the list before; a new one starts now.
func TestStampStarts(t *testing.T) {
	then, now := time.Date(2026, 11, 3, 20, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 21, 0, 0, 0, time.UTC)
	tk := []TurnTask{{ID: "a"}, {ID: "b"}}
	stampStarts(tk, `[{"id":"a","type":"local_bash","started_at":"2026-11-03T20:00:00Z"}]`, now)
	if !tk[0].StartedAt.Equal(then) || !tk[1].StartedAt.Equal(now) {
		t.Fatalf("%v %v", tk[0].StartedAt, tk[1].StartedAt)
	}
}

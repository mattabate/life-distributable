package threads

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"life/hub/internal/progress"
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

// An approved action the hub is running shows on the session that proposed
// it, idle or not, with its newest step; a finished one, or another
// session's, does not.
func TestRunningActionsOnTheSession(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Clean up the old builds.", "", "", nil)
	other, _ := m.Create("", "life", "", "Something else.", "", "", nil)
	for _, r := range [][]string{
		{"act-1", th.ID, "running", "Delete 940 old build folders"},
		{"act-2", th.ID, "done", "Earlier job"},
		{"act-3", other.ID, "running", "Not this session's"},
		{"act-4", th.ID, "running", "A shell job"},
	} {
		if _, err := m.db.Exec(`INSERT INTO items (id,src,created_at,updated_at,title,state,thread_id,resolved_at) VALUES (?,'action','2026-11-05T05:00:00Z','2026-11-05T05:00:00Z',?,?,?,'2026-11-05T05:01:00Z')`, r[0], r[3], r[2], r[1]); err != nil {
			t.Fatal(err)
		}
	}
	ctx := progress.Start(context.Background(), "act-1")
	defer progress.End("act-1")
	progress.Report(ctx, "280 deleted, 12 kept", 312, 940)
	ctx4 := progress.Start(context.Background(), "act-4")
	defer progress.End("act-4")
	io.WriteString(progress.Lines(ctx4), "start\nposted 3/10\rposted 4/10\npart")
	got, err := m.Get(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	k := got.RunningActions
	if len(k) != 2 || k[0].ID != "act-1" || k[0].Type != "action" || k[0].Description != "Delete 940 old build folders" ||
		k[0].StartedAt == nil || !k[0].StartedAt.Equal(time.Date(2026, 11, 5, 5, 1, 0, 0, time.UTC)) ||
		k[0].Doing != "280 deleted, 12 kept" || k[0].Done != 312 || k[0].Total != 940 {
		t.Fatalf("%+v", k)
	}
	if k[1].Doing != "posted 4/10" || k[1].Done != 4 || k[1].Total != 10 {
		t.Fatalf("shell row %+v", k[1])
	}
	list, _ := m.List(false)
	for _, x := range list {
		if n := len(x.RunningActions); (x.ID == th.ID && n != 2) || (x.ID == other.ID && n != 1) {
			t.Fatalf("list %s: %+v", x.ID, x.RunningActions)
		}
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

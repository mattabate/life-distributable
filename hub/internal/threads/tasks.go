package threads

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// A helper's own steps, so a surface can open a subagent and see its work.
// An Agent helper never streams into the session's output, but the CLI
// writes its transcript beside the session's own:
// <Transcripts>/<cwd slug>/<claude_session_id>/subagents/agent-<task id>.jsonl,
// the task id being Thread.TurnTasks[].id. TaskEvents reads it straight from
// there, read-only and never stored, in the shape of Events: each event's id
// is its position in the file (1, 2, ...), so `since` pages forward as the
// helper writes.

var taskIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ErrNoTask: the session has no transcript for that helper.
var ErrNoTask = errors.New("no such task")

// taskTranscript finds the helper's transcript file, "" when there is none.
func (m *Manager) taskTranscript(threadID, taskID string) string {
	taskID = strings.TrimPrefix(taskID, "agent-")
	if m.Transcripts == "" || !taskIDRe.MatchString(taskID) {
		return ""
	}
	var sid string
	m.db.QueryRow(`SELECT COALESCE(claude_session_id,'') FROM threads WHERE id=?`, threadID).Scan(&sid)
	if !taskIDRe.MatchString(sid) {
		return ""
	}
	hits, _ := filepath.Glob(filepath.Join(m.Transcripts, "*", sid, "subagents", "agent-"+taskID+".jsonl"))
	if len(hits) == 0 {
		return ""
	}
	return hits[0]
}

// TaskEvents: the helper's steps after event `since`, at most limit, oldest
// first. ErrNoTask when the session has no such helper transcript.
func (m *Manager) TaskEvents(threadID, taskID string, since int64, limit int) ([]Event, error) {
	path := m.taskTranscript(threadID, taskID)
	if path == "" {
		return nil, ErrNoTask
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrNoTask
	}
	defer f.Close()
	out := []Event{}
	var n int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() && len(out) < limit {
		line := sc.Text()
		evs := parseEvents(line)
		if len(evs) == 0 {
			continue
		}
		var head struct {
			Timestamp time.Time `json:"timestamp"`
		}
		json.Unmarshal([]byte(line), &head)
		for _, ev := range evs {
			n++
			if n <= since || len(out) >= limit {
				continue
			}
			ev.ID, ev.ThreadID, ev.TS = n, threadID, head.Timestamp
			out = append(out, ev)
		}
	}
	return out, nil
}

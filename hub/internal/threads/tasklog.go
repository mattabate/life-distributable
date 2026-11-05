package threads

import (
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"life/hub/internal/progress"
)

// A background shell's log, so a long job a session started can be found and
// read. The CLI writes a local_bash task's output to
// /tmp/claude-<uid>/<cwd slug>/<session>/tasks/<task id>.output, the slug
// being the one its transcripts sit under. The file belongs to the session
// user, so the hub reads it straight when it can and through that user
// otherwise; never stored.

const taskTail = 64 << 10

// taskOutputPath: the shell task's output file, "" when it cannot be named.
func (m *Manager) taskOutputPath(threadID, taskID string) string {
	if m.Transcripts == "" || !taskIDRe.MatchString(taskID) {
		return ""
	}
	var sid string
	m.db.QueryRow(`SELECT COALESCE(claude_session_id,'') FROM threads WHERE id=?`, threadID).Scan(&sid)
	if !taskIDRe.MatchString(sid) {
		return ""
	}
	hits, _ := filepath.Glob(filepath.Join(m.Transcripts, "*", sid+".jsonl"))
	if len(hits) == 0 {
		return ""
	}
	uid := strconv.Itoa(os.Getuid())
	if m.RunAs.User != "" {
		u, err := user.Lookup(m.RunAs.User)
		if err != nil {
			return ""
		}
		uid = u.Uid
	}
	return filepath.Join(m.taskTmp(), "claude-"+uid, filepath.Base(filepath.Dir(hits[0])), sid, "tasks", taskID+".output")
}

// taskTmp: where the CLI keeps its task files (TaskTmp in tests).
func (m *Manager) taskTmp() string {
	if m.TaskTmp != "" {
		return m.TaskTmp
	}
	return "/tmp"
}

// tailFile: the last n bytes of a file, as the hub or else as the session user.
func (m *Manager) tailFile(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if m.RunAs.User == "" {
			return nil, err
		}
		return m.asSession("/usr/bin/tail", "-c", strconv.FormatInt(n, 10), path)
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > n {
		f.Seek(st.Size()-n, io.SeekStart)
	}
	return io.ReadAll(io.LimitReader(f, n))
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// logLines: a tail as lines, a carriage-return redraw (tqdm) kept as its last
// frame, a first line cut by the tail dropped, blank lines and colour codes out.
func logLines(b []byte, cut bool) []string {
	lines := strings.Split(ansiRe.ReplaceAllString(string(b), ""), "\n")
	if cut && len(lines) > 1 {
		lines = lines[1:]
	}
	out := []string{}
	for _, l := range lines {
		l = strings.TrimRight(l, "\r \t")
		if i := strings.LastIndex(l, "\r"); i >= 0 {
			l = l[i+1:]
		}
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// "45/100" or "45 of 100" (a count), else "45%".
var (
	countRe = regexp.MustCompile(`\b(\d{1,7}) ?(?:/|of) ?(\d{1,7})\b`)
	pctRe   = regexp.MustCompile(`\b(\d{1,3}(?:\.\d+)?) ?%`)
)

// progressOf: the done/total a line prints, 0/0 when it prints none (a count
// past its total, or a total of 0, is not progress).
func progressOf(line string) (done, total int) {
	if g := countRe.FindStringSubmatch(line); g != nil {
		d, _ := strconv.Atoi(g[1])
		t, _ := strconv.Atoi(g[2])
		if t > 0 && d <= t {
			return d, t
		}
	}
	if g := pctRe.FindStringSubmatch(line); g != nil {
		if p, err := strconv.ParseFloat(g[1], 64); err == nil && p <= 100 {
			return int(p + 0.5), 100
		}
	}
	return 0, 0
}

// TaskOutput: the newest part of a shell task's log as lines (at most its
// last 64 KB; cut = there is more before it). ErrNoTask when there is none.
func (m *Manager) TaskOutput(threadID, taskID string) (lines []string, cut bool, err error) {
	path := m.taskOutputPath(threadID, taskID)
	if path == "" {
		return nil, false, ErrNoTask
	}
	b, err := m.tailFile(path, taskTail)
	if err != nil {
		return nil, false, ErrNoTask
	}
	cut = len(b) >= taskTail
	return logLines(b, cut), cut, nil
}

type taskLive struct {
	at          time.Time
	doing       string
	done, total int
}

var (
	taskMu   sync.Mutex
	taskSeen = map[string]taskLive{}
)

// fillTasks: each running task's newest line or step and its progress, read
// from its log at most once every few seconds per task (the Sessions list
// polls).
func (m *Manager) fillTasks(t *Thread) {
	for i := range t.TurnTasks {
		k := &t.TurnTasks[i]
		key := t.ID + "/" + k.ID
		taskMu.Lock()
		c, ok := taskSeen[key]
		taskMu.Unlock()
		if !ok || time.Since(c.at) > 4*time.Second {
			c = m.readTask(t.ID, *k)
			taskMu.Lock()
			if len(taskSeen) > 500 {
				taskSeen = map[string]taskLive{}
			}
			taskSeen[key] = c
			taskMu.Unlock()
		}
		k.Doing, k.Done, k.Total = c.doing, c.done, c.total
	}
}

// runningActions: the running approved actions by the session that proposed
// them (one session's when threadID is set), as Thread.RunningActions rows.
func (m *Manager) runningActions(threadID string) map[string][]TurnTask {
	q := `SELECT id, thread_id, title, COALESCE(resolved_at, created_at) FROM items WHERE src='action' AND state='running' AND COALESCE(thread_id,'')<>''`
	args := []any{}
	if threadID != "" {
		q += ` AND thread_id=?`
		args = append(args, threadID)
	}
	rows, err := m.db.Query(q+` ORDER BY created_at`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string][]TurnTask{}
	for rows.Next() {
		var id, thr, title, at string
		if rows.Scan(&id, &thr, &title, &at) != nil {
			continue
		}
		k := TurnTask{ID: id, Type: "action", Description: title}
		if x, err := time.Parse(time.RFC3339Nano, at); err == nil {
			k.StartedAt = &x
		}
		if l, ok := progress.Get(id); ok {
			k.Doing, k.Done, k.Total = ansiRe.ReplaceAllString(l.Doing, ""), l.Done, l.Total
			if k.Total == 0 {
				k.Done, k.Total = progressOf(k.Doing)
			}
		}
		out[thr] = append(out[thr], k)
	}
	return out
}

func (m *Manager) readTask(threadID string, k TurnTask) taskLive {
	c := taskLive{at: time.Now()}
	if k.Type == "local_agent" {
		c.doing = m.agentDoing(threadID, k.ID)
	} else if lines, _, err := m.TaskOutput(threadID, k.ID); err == nil {
		for j := len(lines) - 1; j >= 0 && j >= len(lines)-20; j-- {
			if d, n := progressOf(lines[j]); n > 0 {
				c.done, c.total = d, n
				break
			}
		}
		if len(lines) > 0 {
			c.doing = lines[len(lines)-1]
		}
	}
	if r := []rune(c.doing); len(r) > 160 {
		c.doing = string(r[:159]) + "…"
	}
	return c
}

// agentDoing: a helper's newest tool call, from the tail of its transcript.
func (m *Manager) agentDoing(threadID, taskID string) string {
	path := m.taskTranscript(threadID, taskID)
	if path == "" {
		return ""
	}
	b, err := m.tailFile(path, 256<<10)
	if err != nil {
		return ""
	}
	doing := ""
	for _, line := range strings.Split(string(b), "\n") {
		for _, ev := range parseEvents(line) {
			if ev.Kind == "tool_use" {
				if doing = ev.Summary; doing == "" {
					doing = ev.Title
				}
			}
		}
	}
	return doing
}

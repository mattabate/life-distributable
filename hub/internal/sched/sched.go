// Package sched runs the job table (ops/schedule.json): headless Claude
// jobs whose output is routed to the actions queue (findings) and the asks
// board (needs_you); the summary stays on the run record. It is also the
// hub's one Notifier: NEEDS YOU = a push to the phone, nothing else.
//
// It has no clock of its own (one clock): each job's cadence is
// a standing prompt row (target job:<name>) that Reconcile derives from the
// table and the threads clock fires through Fire.
package sched

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"life/hub/internal/actions"
	"life/hub/internal/cadence"
	"life/hub/internal/format"
	"life/hub/internal/spend"
	"life/hub/internal/store"
)

type JobSpec struct {
	Name    string `json:"name"`
	Project string `json:"project"`
	// When: "daily@HH:MM" | "weekly@Mon HH:MM" | "every@<duration>" (e.g. 6h) | "manual".
	When   string `json:"when"`
	Prompt string `json:"prompt"`
	// Timeout for the claude run (default 20m).
	Timeout string `json:"timeout"`
	Enabled *bool  `json:"enabled"`
	// Gate: a named runtime condition checked on top of When ("asks" = the
	// ask verifier: only when there are machine-checkable open asks and
	// something happened since the last run). Evaluated by Scheduler.Gate.
	Gate string `json:"gate"`
	// AllowedTools overrides Table.DefaultTools for this job.
	AllowedTools []string `json:"allowed_tools"`
}

type Table struct {
	Jobs []JobSpec `json:"jobs"`
	// DefaultTools: Claude Code --allowedTools for jobs. Unattended jobs get
	// read-only shell + lifectl; never bypassPermissions (the gate must stay
	// structural). acceptEdits, but a file tool the grant does not name is
	// passed as --disallowedTools (jobDenied).
	DefaultTools []string `json:"default_tools"`
	// PromptTools: allowlist for sessions (threads) — turns the owner starts
	// from either surface. They asked for them, so they may build/install/commit.
	// Still acceptEdits + allowlist, never bypassPermissions.
	PromptTools []string `json:"prompt_tools"`
}

func LoadTable(path string) (Table, error) {
	var t Table
	b, err := os.ReadFile(path)
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return t, fmt.Errorf("%s: %w", path, err)
	}
	// The fallbacks below apply when schedule.json omits the key — including
	// `"default_tools": []`, which is how the live file asks for "the safe
	// default", not "nothing". So they must not re-grant what schedule.json
	// deliberately dropped: no `python3:*`, `sqlite3:*` or `curl` in either
	// list. Reads go through
	// ops/db.sh (sqlite3 -readonly), hub calls through `lifectl api`, and
	// ops/py.sh runs a reviewed script from ops/ rather than any code at all.
	if len(t.PromptTools) == 0 {
		t.PromptTools = []string{"Read", "Grep", "Glob", "Edit", "Write", "WebSearch", "WebFetch",
			"Bash(lifectl:*)", "Bash(git status:*)", "Bash(git log:*)", "Bash(git diff:*)", "Bash(git add:*)", "Bash(git commit:*)",
			"Bash(make:*)", "Bash(ops/install-phone.sh:*)", "Bash(ops/hub.sh:*)", "Bash(go:*)", "Bash(gofmt:*)", "Bash(xcodegen:*)", "Bash(xcodebuild:*)",
			"Bash(xcrun simctl:*)", "Bash(xcrun devicectl:*)", "Bash(xcrun xcresulttool:*)", "Bash(xcrun dyld_info:*)", "Bash(xcrun atos:*)",
			"Bash(ls:*)", "Bash(cat:*)", "Bash(head:*)", "Bash(tail:*)", "Bash(grep:*)", "Bash(wc:*)", "Bash(date:*)",
			"Bash(ops/db.sh:*)", "Bash(ops/py.sh:*)"}
	}
	if len(t.DefaultTools) == 0 {
		t.DefaultTools = []string{"Read", "Grep", "Glob", "WebSearch", "WebFetch",
			"Bash(lifectl:*)", "Bash(git status:*)", "Bash(git log:*)", "Bash(git diff:*)",
			"Bash(ls:*)", "Bash(cat:*)", "Bash(head:*)", "Bash(tail:*)", "Bash(grep:*)",
			"Bash(date:*)", "Bash(wc:*)", "Bash(ops/db.sh:*)"}
	}
	for _, j := range t.Jobs {
		if j.Name == "" || j.Prompt == "" {
			return t, fmt.Errorf("job needs name and prompt")
		}
		if _, _, err := parseWhen(j.When); err != nil {
			return t, fmt.Errorf("job %s: %w", j.Name, err)
		}
	}
	return t, nil
}

// Scheduler state. Runs are recorded in the `runs` table.
type Scheduler struct {
	table     Table
	tablePath string
	db        *store.DB
	acts      *actions.Queue
	claudeBin string
	projDir   func(string) (string, bool)
	// Push: APNs to the app (nil = none). The one notification channel: a
	// push that opens Your turn. Errors are logged, never fatal.
	Push actions.Notifier
	// Ask turns a job's needs_you line into an ask card (nil = log only). A
	// job has no session, so main.go points this at an idle "hub" thread —
	// the same shape as calendar's fallback thread — so the line lands on
	// Your turn like every other thing an agent needs from the owner, instead
	// of in a digest nobody reads.
	Ask       func(job, text string)
	Now       func() time.Time
	RunClaude func(ctx context.Context, dir, prompt string, tools []string) (Output, error)

	// Gate answers JobSpec.Gate for due jobs (nil = no gating). A shut gate
	// holds the wake: Fire answers "held" and the clock asks again next tick.
	Gate func(gate string, last time.Time) bool
	// Allow is the budget guard for unattended runs (kind "job", id = name):
	// a refused wake is recorded as a skipped run so it is not retried every
	// minute. nil = always. `lifectl job run` (via api|cli) is never gated.
	Allow func(kind, id string) (bool, string)
	// clock is the hub's one clock (threads.Manager): a job's cadence is a
	// standing prompt row with target job:<name>, reconciled from the table.
	clock Clock
	// ModelArgs: the --model/--effort/--max-budget-usd flags for
	// a job's `claude -p`, from the model policy (spend/policy.go) through
	// the ladder picker. nil = the CLI's defaults.
	ModelArgs func() []string

	mu      sync.Mutex
	running map[string]bool
}

// Output is what a job's Claude run must return (it is instructed to emit
// exactly this JSON as its final answer).
type Output struct {
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
	NeedsYou []string  `json:"needs_you"`
	Raw      string    `json:"-"`
	// Cost: the CLI's total_cost_usd for the run (recorded on runs.cost_usd).
	Cost float64 `json:"-"`
}

// Finding: one thing a job wants done; gated kinds become proposals.
type Finding struct {
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// Optional executable follow-up for the actions queue.
	ExecType    string          `json:"exec_type"`
	ExecPayload json.RawMessage `json:"exec_payload"`
}

// RunDetail is one run read back for a person: the Run row plus what the
// model actually said — the prose before its JSON envelope (`text`) and the
// envelope's parts. A job has no chat, so this is what a proposal from a
// scheduled job opens on — the chat where the approval was suggested; the phone and the console draw this, not `output`
// (the CLI's raw result JSON, kept for debugging).
type RunDetail struct {
	Run
	Text     string    `json:"text,omitempty"`
	Findings []Finding `json:"findings"`
	NeedsYou []string  `json:"needs_you"`
}

// GetRun reads one run and parses its stored output. A failed run (no JSON
// envelope, a claude error) comes back with the Run row alone.
func (s *Scheduler) GetRun(id int64) (RunDetail, error) {
	rs, err := s.runs(`WHERE id=?`, 1, id)
	if err != nil {
		return RunDetail{}, err
	}
	if len(rs) == 0 {
		return RunDetail{}, fmt.Errorf("no run %d", id)
	}
	d := RunDetail{Run: rs[0], Findings: []Finding{}, NeedsYou: []string{}}
	var env struct {
		Result string `json:"result"`
	}
	if json.Unmarshal([]byte(d.Output), &env) != nil || env.Result == "" {
		return d, nil
	}
	if o, perr := ParseOutput(env.Result, ""); perr == nil {
		if o.Findings != nil {
			d.Findings = o.Findings
		}
		if o.NeedsYou != nil {
			d.NeedsYou = o.NeedsYou
		}
		if d.Summary == "" {
			d.Summary = o.Summary
		}
		d.Text = proseBefore(env.Result)
	} else {
		d.Text = strings.TrimSpace(env.Result)
	}
	return d, nil
}

// Schema: job runs (store/migrate.go). The first statement still creates the
// digest queue because a recorded statement is never edited; the last one
// drops it — the daily-digest lane (iMessage batch at digest_at) was removed.
// Nothing reads the table.
var Schema = []string{`CREATE TABLE IF NOT EXISTS runs (
	id INTEGER PRIMARY KEY, job TEXT NOT NULL, started_at TEXT NOT NULL, finished_at TEXT,
	ok INTEGER, summary TEXT, output TEXT, error TEXT, cost_usd REAL, session_id TEXT);
CREATE INDEX IF NOT EXISTS runs_job ON runs(job, started_at);
CREATE TABLE IF NOT EXISTS digest (id INTEGER PRIMARY KEY, created_at TEXT NOT NULL, text TEXT NOT NULL, flushed_at TEXT);`,
	`DROP TABLE IF EXISTS digest`,
	// prompt_id: the wake (a child prompt row) this run answers — '' for
	// `lifectl job run` and runs made before the column existed.
	`ALTER TABLE runs ADD COLUMN prompt_id TEXT NOT NULL DEFAULT ''`}

// Clock is what the scheduler needs from the one clock (threads.Manager):
// the standing prompt rows that carry every job's cadence.
type Clock interface {
	SetStanding(author, target, repeat, text string, last time.Time) error
	StandingRepeat(target string) (string, bool)
	StandingTargets(prefix string) []string
}

// SetClock hands the scheduler the one clock and reconciles the table into
// it. From here on the table is read for a job's prompt and tools; WHEN it
// fires is the standing row (fired by the clock through Fire).
func (s *Scheduler) SetClock(c Clock) {
	s.clock = c
	if err := s.Reconcile(); err != nil {
		log.Printf("sched: reconcile: %v", err)
	}
}

// Reconcile makes the standing rows match the table: one queued row per
// enabled clock job (target job:<name>, repeat = when), none for manual or
// disabled jobs, none for jobs no longer in the table. An unchanged cadence
// is left alone, so its not_before survives a reload; a changed one counts
// from the job's last run.
func (s *Scheduler) Reconcile() error {
	if s.clock == nil {
		return nil
	}
	s.mu.Lock()
	t := s.table
	s.mu.Unlock()
	want, text := map[string]string{}, map[string]string{}
	for _, j := range t.Jobs {
		w := ""
		if (j.Enabled == nil || *j.Enabled) && cadence.Kind(j.When) == "clock" {
			w = j.When
		}
		want["job:"+j.Name] = w
		// The row carries the prompt's first line so the agenda can say what
		// the job is without reaching back into the table (the prompt itself is
		// still the table's; runWith reads it there).
		text["job:"+j.Name] = truncate(strings.SplitN(strings.TrimSpace(j.Prompt), "\n", 2)[0], 200)
	}
	for _, target := range s.clock.StandingTargets("job:") {
		if _, ok := want[target]; !ok {
			want[target] = ""
		}
	}
	var firstErr error
	for target, w := range want {
		have, _ := s.clock.StandingRepeat(target)
		if have == w {
			continue
		}
		if err := s.clock.SetStanding("hub", target, w, text[target], s.lastRun(strings.TrimPrefix(target, "job:"))); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Held: would a wake of this job hold right now (a run still going, or its
// gate shut)? Read-only, so the clock can ask before it claims an occurrence
// and a job held for days writes nothing each minute. Unknown job: not held
// (Fire answers "gone").
func (s *Scheduler) Held(name string) (bool, string) {
	s.mu.Lock()
	var gate string
	for _, j := range s.table.Jobs {
		if j.Name == name {
			gate = j.Gate
		}
	}
	running := s.running[name]
	s.mu.Unlock()
	if running {
		return true, "already running"
	}
	if gate != "" && s.Gate != nil && !s.Gate(gate, s.lastRun(name)) {
		return true, "gate " + gate + " is shut"
	}
	return false, ""
}

// Fire is the clock's way to run a job when its standing row comes due.
// The answer is what happened to the wake: "started" (a run row exists,
// carrying promptID), "held" (gate shut or a run of it still going — nothing
// is written and the clock asks again next tick), "skipped" (the budget
// guard refused: a skipped run row records why) or "gone" (no such job).
func (s *Scheduler) Fire(name, promptID string) (state, why string) {
	if held, why := s.Held(name); held {
		return "held", why
	}
	s.mu.Lock()
	var spec *JobSpec
	for i := range s.table.Jobs {
		if s.table.Jobs[i].Name == name {
			spec = &s.table.Jobs[i]
		}
	}
	s.mu.Unlock()
	if spec == nil {
		return "gone", "no job " + name
	}
	if s.Allow != nil {
		if ok, why := s.Allow("job", name); !ok {
			log.Printf("sched: %s skipped: %s", name, why)
			fin := store.TS(s.Now())
			s.db.Exec(`INSERT INTO runs (job, started_at, finished_at, ok, error, prompt_id) VALUES (?,?,?,0,?,?)`, name, fin, fin, "skipped by the hub: "+why, promptID)
			return "skipped", why
		}
	}
	// The run row is written before Fire answers, so "started" means a row
	// exists. Begun in the goroutine, a run that could not start (a project
	// the hub config does not have, a job started by hand a moment before)
	// returned its error to nobody: the wake read "delivered" with no run
	// row and no log line saying why.
	b, err := s.begin(name, "schedule", promptID)
	if err != nil {
		if b.id == 0 {
			return "held", err.Error() // already running: the next tick asks again
		}
		log.Printf("sched: %s not started: %v", name, err)
		return "skipped", err.Error()
	}
	go s.exec(b)
	return "started", ""
}

func New(db *store.DB, tablePath string, acts *actions.Queue, claudeBin string, projDir func(string) (string, bool)) (*Scheduler, error) {
	if _, err := db.Migrate("sched", Schema); err != nil {
		return nil, err
	}
	t, err := LoadTable(tablePath)
	if err != nil {
		return nil, err
	}
	// A run is a child of this process: one still open at startup died with
	// the last hub and will never be closed by anyone else.
	if _, err := db.Exec(`UPDATE runs SET finished_at=?, ok=0, error='hub restarted mid-run' WHERE finished_at IS NULL`, store.TS(time.Now())); err != nil {
		log.Printf("sched: closing orphaned runs: %v", err)
	}
	s := &Scheduler{table: t, tablePath: tablePath, db: db, acts: acts, claudeBin: claudeBin, projDir: projDir,
		Now: time.Now, running: map[string]bool{}}
	s.RunClaude = s.runClaude
	return s, nil
}

func (s *Scheduler) Table() Table { return s.table }

// SetActions breaks the construction cycle (queue notifies via scheduler).
func (s *Scheduler) SetActions(q *actions.Queue) { s.acts = q }

// Reload re-reads the table (called on SIGHUP / API).
func (s *Scheduler) Reload() error {
	t, err := LoadTable(s.tablePath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.table = t
	s.mu.Unlock()
	return s.Reconcile()
}

// Next returns the next time `when` fires after now, given the last run.
// Zero time for manual/invalid schedules. The grammar lives in
// internal/cadence (one clock); this is the name callers know.
func Next(when string, last, now time.Time) time.Time { return cadence.Next(when, last, now) }

// parseWhen splits a clock cadence after cadence has validated it.
func parseWhen(w string) (kind, arg string, err error) {
	if w == "manual" || w == "" {
		return "manual", "", nil
	}
	if cadence.Kind(w) != "clock" {
		return "", "", fmt.Errorf("bad when %q", w)
	}
	kind, arg, _ = strings.Cut(w, "@")
	return kind, arg, nil
}

func (s *Scheduler) lastRun(job string) time.Time {
	var t string
	s.db.QueryRow(`SELECT started_at FROM runs WHERE job=? ORDER BY started_at DESC LIMIT 1`, job).Scan(&t)
	ts, _ := time.Parse(time.RFC3339Nano, t)
	return ts
}

type Run struct {
	ID         int64      `json:"id"`
	Job        string     `json:"job"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	OK         *bool      `json:"ok,omitempty"`
	Summary    string     `json:"summary,omitempty"`
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	CostUSD    float64    `json:"cost_usd"`
	SessionID  string     `json:"session_id,omitempty"`
	// PromptID: the wake this run answered (a child prompt row of the job's
	// standing row); empty for `lifectl job run`.
	PromptID string `json:"prompt_id,omitempty"`
}

func (s *Scheduler) Runs(job string, limit int) ([]Run, error) {
	if job != "" {
		return s.runs(`WHERE job=?`, limit, job)
	}
	return s.runs(``, limit)
}

func (s *Scheduler) runs(where string, limit int, args ...any) ([]Run, error) {
	q := `SELECT id,job,started_at,finished_at,ok,summary,output,error,cost_usd,session_id,prompt_id FROM runs ` + where +
		fmt.Sprintf(` ORDER BY started_at DESC LIMIT %d`, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		var st string
		var fin, sum, outp, errS, sid *string
		var ok *int
		var cost *float64
		if err := rows.Scan(&r.ID, &r.Job, &st, &fin, &ok, &sum, &outp, &errS, &cost, &sid, &r.PromptID); err != nil {
			return nil, err
		}
		r.StartedAt, _ = time.Parse(time.RFC3339Nano, st)
		if fin != nil {
			t, _ := time.Parse(time.RFC3339Nano, *fin)
			r.FinishedAt = &t
		}
		if ok != nil {
			b := *ok == 1
			r.OK = &b
		}
		for dst, src := range map[*string]*string{&r.Summary: sum, &r.Output: outp, &r.Error: errS, &r.SessionID: sid} {
			if src != nil {
				*dst = *src
			}
		}
		if cost != nil {
			r.CostUSD = *cost
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Run executes one job now (via = schedule|api|cli). Serialised per job.
func (s *Scheduler) Run(name, via string) (int64, error) { return s.runWith(name, via, "") }

func (s *Scheduler) runWith(name, via, promptID string) (int64, error) {
	b, err := s.begin(name, via, promptID)
	if err != nil {
		return b.id, err
	}
	return s.exec(b)
}

// begun is a run begin opened: the job holds its running flag and run row
// id stays open until exec closes both. On a begin error, id is the failed
// run row it wrote instead (0 = none, e.g. the job was already running).
type begun struct {
	spec     JobSpec
	dir, via string
	id       int64
}

// begin claims the job's running flag and opens its run row. A job whose
// project the hub config lacks gets a failed run row saying so (it would
// fail the same way every occurrence, so it must be on the record), and the
// flag is released.
func (s *Scheduler) begin(name, via, promptID string) (begun, error) {
	var spec *JobSpec
	s.mu.Lock()
	for i := range s.table.Jobs {
		if s.table.Jobs[i].Name == name {
			spec = &s.table.Jobs[i]
		}
	}
	if spec == nil {
		s.mu.Unlock()
		return begun{}, fmt.Errorf("no job %q", name)
	}
	if s.running[name] {
		s.mu.Unlock()
		return begun{}, fmt.Errorf("job %q already running", name)
	}
	s.running[name] = true
	j := *spec
	s.mu.Unlock()
	release := func() { s.mu.Lock(); delete(s.running, name); s.mu.Unlock() }

	now := store.TS(s.Now())
	dir, ok := s.projDir(j.Project)
	if !ok {
		release()
		err := fmt.Errorf("job %s: unknown project %q", name, j.Project)
		res, ierr := s.db.Exec(`INSERT INTO runs (job, started_at, finished_at, ok, error, prompt_id) VALUES (?,?,?,0,?,?)`, name, now, now, err.Error(), promptID)
		if ierr != nil {
			return begun{}, err
		}
		id, _ := res.LastInsertId()
		return begun{id: id}, err
	}
	res, err := s.db.Exec(`INSERT INTO runs (job, started_at, prompt_id) VALUES (?,?,?)`, name, now, promptID)
	if err != nil {
		release()
		return begun{}, err
	}
	id, _ := res.LastInsertId()
	return begun{spec: j, dir: dir, via: via, id: id}, nil
}

// exec runs an open run to its end, closes its row and releases the job.
func (s *Scheduler) exec(b begun) (int64, error) {
	spec, name, dir, via, id := &b.spec, b.spec.Name, b.dir, b.via, b.id
	defer func() { s.mu.Lock(); delete(s.running, name); s.mu.Unlock() }()
	timeout := 20 * time.Minute
	if d, err := time.ParseDuration(spec.Timeout); err == nil && d > 0 {
		timeout = d
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	log.Printf("sched: run %s (%s) via %s", name, spec.Project, via)
	tools := spec.AllowedTools
	if len(tools) == 0 {
		tools = s.table.DefaultTools
	}
	out, err := s.RunClaude(ctx, dir, s.wrapPrompt(*spec)+s.deniedNote(name, id), tools)
	if d := deniedOf(out.Raw); len(d) > 0 {
		log.Printf("sched: job %s: %d tool call(s) refused by its grant: %s", name, len(d), strings.Join(d, " | "))
	}
	fin := store.TS(s.Now())
	if err != nil {
		if _, uerr := s.db.Exec(`UPDATE runs SET finished_at=?, ok=0, error=?, output=?, cost_usd=? WHERE id=?`, fin, err.Error(), out.Raw, out.Cost, id); uerr != nil {
			log.Printf("sched: run %d not closed: %v", id, uerr)
		}
		log.Printf("sched: job %s failed: %v", name, err)
		// The model worked and answered in prose: whatever it found is in
		// the run's output and nowhere else. One card says so (the same
		// title supersedes itself while it stays open), rather than a
		// failed run row nobody reads: a parse failure is a finding.
		if s.Ask != nil && strings.Contains(err.Error(), "JSON") {
			s.Ask(name, fmt.Sprintf("The %s job ended without its result block: read run %d, its findings reached no card", name, id))
		}
		return id, err
	}
	if _, uerr := s.db.Exec(`UPDATE runs SET finished_at=?, ok=1, summary=?, output=?, cost_usd=? WHERE id=?`, fin, out.Summary, out.Raw, out.Cost, id); uerr != nil {
		log.Printf("sched: run %d not closed: %v", id, uerr)
	}
	s.route(*spec, out, id)
	return id, nil
}

// route turns job output into actions (findings) and asks (needs_you); the
// summary stays on the run record. runID rides each proposal as its `run_id`
// — a job has no session, so the run is the only place a proposal can open
// on (GET /runs/{id}).
func (s *Scheduler) route(j JobSpec, o Output, runID int64) {
	for _, f := range o.Findings {
		et := f.ExecType
		if et == "" {
			et = "none"
		}
		if _, err := s.acts.Propose(actions.Proposal{Project: j.Project, Kind: f.Kind, Title: f.Title, Detail: f.Detail,
			ExecType: et, ExecPayload: f.ExecPayload, Source: "claude:job:" + j.Name, RunID: strconv.FormatInt(runID, 10)}); err != nil {
			log.Printf("sched: propose from %s: %v", j.Name, err)
		}
	}
	for _, n := range o.NeedsYou {
		if s.Ask != nil {
			s.Ask(j.Name, n)
		} else {
			log.Printf("sched: %s needs you: %s", j.Name, n)
		}
	}
}

// NeedsYou is the hub's one notification: a push to the phone. Nothing is
// queued or batched; what the push points at (an ask, a proposal) is the
// record, so a lost push loses nothing.
func (s *Scheduler) NeedsYou(text string) error {
	if s.Push != nil {
		if err := s.Push.NeedsYou(text); err != nil {
			log.Printf("push: %v", err)
		}
	}
	return nil
}

// ToRead is the quieter push for a read card: not time-sensitive, so a Focus
// holds it, but Siri still reads it into the owner's headphones. A
// Push without it (a test stub) sends nothing.
func (s *Scheduler) ToRead(text string) error {
	if p, ok := s.Push.(interface{ ToRead(string) error }); ok {
		if err := p.ToRead(text); err != nil {
			log.Printf("push: %v", err)
		}
	}
	return nil
}

// Card pushes a card: `say`, when the session wrote one, is the sentence the
// push speaks instead of the label and title (notify/voice.go); `thread` is
// the session it belongs to, so the list can mark it "speaking" while the
// line is heard; `card` is the card's id,
// so the line outlives a hub restart until it is heard (notify/queue.go). A
// Push without that lane gets the plain NeedsYou / ToRead.
func (s *Scheduler) Card(kind, line, say, thread, card string) error {
	if p, ok := s.Push.(interface {
		PushCard(kind, line, say, thread, card string) error
	}); ok {
		if err := p.PushCard(kind, line, say, thread, card); err != nil {
			log.Printf("push: %v", err)
		}
		return nil
	}
	if kind == "read" {
		return s.ToRead(line)
	}
	return s.NeedsYou(line)
}

func (s *Scheduler) wrapPrompt(j JobSpec) string {
	return j.Prompt + `

---
You are running as the scheduled job "` + j.Name + `" for the owner's life hub, unattended.
Rules:
- Do the work yourself. Only surface things the owner must personally do.
- NEVER move money, delete anything, contact anyone, share data or commit the owner to anything directly. Instead return such steps as findings with the right kind (money|delete|contact|share|commit); the hub will ask the owner for approval.
- Your FINAL message must be ONLY a JSON object, no prose, no code fence:
{"summary": "1-3 sentences on what you did (kept on the run record), or empty string if nothing noteworthy",
 "findings": [{"kind":"money|delete|contact|share|commit|other","title":"...","detail":"what exactly will happen and why","exec_type":"none|shell|claude","exec_payload":{}}],
 "needs_you": ["things only the owner can do, each one line; empty if none"]}`
}

// deniedNote tells a run which tool calls its job's previous run had refused
// by the grant. The CLI lists them in the result's permission_denials; a
// call that cannot pass costs a turn each run. `self` is this run's own
// (still open) row.
func (s *Scheduler) deniedNote(job string, self int64) string {
	var raw string
	s.db.QueryRow(`SELECT COALESCE(output,'') FROM runs WHERE job=? AND id<>? AND finished_at IS NOT NULL AND COALESCE(output,'')<>'' ORDER BY started_at DESC LIMIT 1`, job, self).Scan(&raw)
	d := deniedOf(raw)
	if len(d) == 0 {
		return ""
	}
	return "\n\nYour previous run of this job had these tool calls refused by its tool grant. They will be refused again, so do not retry them; use one plain command the grant allows for each step:\n- " + strings.Join(d, "\n- ")
}

// maxDenied: how many refused calls a note or log line names.
const maxDenied = 5

// deniedOf: the refused tool calls in a CLI result envelope, one line each
// ("Bash: <command>"), deduped, at most maxDenied. Not an envelope: none.
func deniedOf(raw string) []string {
	var env struct {
		Denials []struct {
			Tool  string          `json:"tool_name"`
			Input json.RawMessage `json:"tool_input"`
		} `json:"permission_denials"`
	}
	if json.Unmarshal([]byte(raw), &env) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range env.Denials {
		in := string(d.Input)
		var b struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(d.Input, &b) == nil && b.Command != "" {
			in = b.Command
		}
		line := d.Tool + ": " + truncate(strings.Join(strings.Fields(in), " "), 160)
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
		if len(out) == maxDenied {
			break
		}
	}
	return out
}

// jobEnv puts the hub binary's dir (ops/bin, where lifectl lives) first on
// PATH, as sessions get it: the grant is Bash(lifectl:*), so lifectl must
// resolve bare, and a launchd PATH has no ops/bin.
func jobEnv() []string {
	env := os.Environ()
	exe, err := os.Executable()
	if err != nil || !fileExists(filepath.Join(filepath.Dir(exe), "lifectl")) {
		return env
	}
	return append(env, "PATH="+filepath.Dir(exe)+":"+os.Getenv("PATH"))
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// jobArgs: the CLI line for an unattended job; no MCP server, no connector
// (actions.NoMCP).
func jobArgs(dir string, tools, model []string) []string {
	args := append([]string{"-p", "--output-format", "json", "--permission-mode", "acceptEdits"}, actions.NoMCP...)
	if len(tools) > 0 {
		args = append(args, "--allowedTools", strings.Join(tools, ","))
	}
	if d := jobDenied(dir, tools); len(d) > 0 {
		args = append(args, "--disallowedTools", strings.Join(d, ","))
	}
	return append(args, model...)
}

// jobDenied: the file tools a job may not use. acceptEdits auto-accepts
// Edit/Write anywhere under the job's directory whatever --allowedTools
// says, so a read-only job could rewrite the project, ops/secrets and .git
// included. A file tool the grant never names is refused outright; one it
// names keeps its grant but never reaches ops/secrets or .git, the same
// denials a session gets (threads deniedTools).
func jobDenied(dir string, tools []string) []string {
	var out []string
	for _, t := range []string{"Edit", "Write", "MultiEdit", "NotebookEdit"} {
		named := false
		for _, g := range tools {
			if g == t || strings.HasPrefix(g, t+"(") {
				named = true
				break
			}
		}
		if !named {
			out = append(out, t)
			continue
		}
		if dir == "" {
			continue
		}
		for _, p := range []string{filepath.Join(dir, "ops", "secrets"), filepath.Join(dir, ".git")} {
			out = append(out, t+"(/"+p+"/**)")
		}
	}
	return out
}

func (s *Scheduler) runClaude(ctx context.Context, dir, prompt string, tools []string) (Output, error) {
	var model []string
	if s.ModelArgs != nil {
		model = s.ModelArgs()
	}
	c := exec.CommandContext(ctx, s.claudeBin, jobArgs(dir, tools, model)...)
	c.Dir = dir
	c.Env = jobEnv()
	c.Stdin = strings.NewReader(prompt)
	raw, err := runGroup(c, pipeGrace)
	return decodeResult(raw, err)
}

// pipeGrace: how long a job's output pipe may stay open after claude itself
// exited (or its timeout fired) before the hub stops reading it.
var pipeGrace = 10 * time.Second

// runGroup runs c in its own process group and returns its combined output.
// CombinedOutput alone waits until every holder of the pipe closes it, and a
// tool's leftover child (a backgrounded shell) holds it after claude has
// written its result and exited: the timeout only kills claude, so a run
// could stay open far past its timeout with the job held "already running".
// Now the timeout kills the whole group, the pipe is dropped `grace` after
// claude exits, and any straggler is killed then.
func runGroup(c *exec.Cmd, grace time.Duration) ([]byte, error) {
	var buf bytes.Buffer
	c.Stdout, c.Stderr = &buf, &buf
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = grace
	err := c.Run()
	if c.Process != nil {
		syscall.Kill(-c.Process.Pid, syscall.SIGKILL) // stragglers; claude is reaped
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil // claude exited 0; only an orphan held the pipe
	}
	return buf.Bytes(), err
}

// decodeResult reads the CLI's one JSON result from a job's output. A run
// that fails exits 1 WITH that envelope, and its reason (an API error, a
// limit) sits about 1 KB in, after usage and subagent_stats: the first 500
// raw bytes kept as the error never reached it, and the run's cost was
// dropped. So the envelope is read on a nonzero exit too; only output that
// is not one is quoted raw.
func decodeResult(raw []byte, runErr error) (Output, error) {
	o := Output{Raw: string(raw)}
	var env struct {
		Result    string  `json:"result"`
		IsError   bool    `json:"is_error"`
		Cost      float64 `json:"total_cost_usd"`
		SessionID string  `json:"session_id"`
		APIStatus any     `json:"api_error_status"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		if runErr != nil {
			return o, fmt.Errorf("claude: %v: %s", runErr, truncate(string(raw), 500))
		}
		return o, fmt.Errorf("claude output not json: %s", truncate(string(raw), 300))
	}
	o.Cost = env.Cost
	if env.IsError || runErr != nil {
		why := strings.TrimSpace(env.Result)
		if env.APIStatus != nil {
			why += fmt.Sprintf(" (API status %v)", env.APIStatus)
		}
		if why == "" {
			why = "no reason given"
		}
		if runErr != nil {
			return o, fmt.Errorf("claude error (%v): %s", runErr, truncate(why, 500))
		}
		return o, fmt.Errorf("claude error: %s", truncate(why, 500))
	}
	out, err := ParseOutput(env.Result, string(raw))
	out.Cost = env.Cost
	return out, err
}

// CostByJob: dollars the scheduled jobs spent since `since`, by job name
// (the CLI's total_cost_usd per run; older runs may carry none).
func (s *Scheduler) CostByJob(since time.Time) []spend.Bucket {
	rows, err := s.db.Query(`SELECT job, COUNT(*), COALESCE(SUM(cost_usd),0) FROM runs WHERE started_at>=? AND cost_usd>0 GROUP BY job ORDER BY 3 DESC`, store.TS(since))
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []spend.Bucket{}
	for rows.Next() {
		var b spend.Bucket
		if rows.Scan(&b.Key, &b.Messages, &b.USD) == nil {
			out = append(out, b)
		}
	}
	return out
}

// ParseOutput extracts the JSON object from the model's final text: the
// last top-level object carrying an envelope key (summary/findings/
// needs_you). Prose before it may quote JSON of its own, so the first "{" to
// the last "}" is not always the envelope.
func ParseOutput(text, raw string) (Output, error) {
	o := Output{Raw: raw}
	if at, n := envelopeAt(text); at >= 0 {
		if err := json.Unmarshal([]byte(text[at:at+n]), &o); err == nil {
			return o, nil
		}
	}
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return o, fmt.Errorf("no JSON object in result: %s", truncate(text, 300))
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &o); err != nil {
		return o, fmt.Errorf("result JSON: %v: %s", err, truncate(text, 300))
	}
	return o, nil
}

// envelopeAt finds the last top-level JSON object in text that has an
// envelope key: its offset and length, or -1. An object that decodes is
// skipped whole, so a finding's nested exec_payload is never a candidate.
func envelopeAt(text string) (at, n int) {
	at = -1
	for i := strings.IndexByte(text, '{'); i >= 0; {
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		var m map[string]json.RawMessage
		next := i + 1
		if dec.Decode(&m) == nil {
			end := int(dec.InputOffset())
			_, s := m["summary"]
			_, f := m["findings"]
			_, y := m["needs_you"]
			if s || f || y {
				at, n = i, end
			}
			next = i + end
		}
		j := strings.IndexByte(text[next:], '{')
		if j < 0 {
			break
		}
		i = next + j
	}
	return at, n
}

// proseBefore is the model's text ahead of the object ParseOutput read.
func proseBefore(text string) string {
	at, _ := envelopeAt(text)
	if at < 0 {
		at = max(strings.Index(text, "{"), 0)
	}
	return strings.TrimSpace(text[:at])
}

func truncate(s string, n int) string { return format.Truncate(s, n) }

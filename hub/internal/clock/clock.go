// Package clock is the hub's one ticker. Every periodic loop that used to be
// its own `go func() { for { work; time.Sleep(d) } }()` in main.go registers
// here instead (2026-08-28, docs/reviews/2026-08-28-one-clock.md): one
// goroutine wakes once a minute, starts whatever is due, and remembers when
// each task last started, finished, and failed — so `/status` can say when
// each loop last ran instead of the owner inferring it from the log.
//
// A task runs in its own goroutine and never overlaps itself: if it is still
// running when its next turn comes, that turn is skipped and it is due again
// on the first tick after it finishes. The next due time is taken from the
// tick that started it, so a 1-minute task started on a 1-minute ticker is
// due on every tick, not every other one.
//
// What is NOT here: `threads.Poll` (3s) is a process pump, not a clock, and
// stays a plain loop. The prompts table is the schedule of every agent wake;
// this package is only the schedule of the hub's own housekeeping.
package clock

import (
	"errors"
	"log"
	"strconv"
	"sync"
	"time"
)

// Task is one registered loop as /status reports it.
type Task struct {
	Name      string     `json:"name"`
	Every     string     `json:"every"` // human: "1m", "6h"
	NextDue   time.Time  `json:"next_due"`
	LastStart *time.Time `json:"last_start,omitempty"`
	LastEnd   *time.Time `json:"last_end,omitempty"`
	Running   bool       `json:"running"`
	LastError string     `json:"last_error,omitempty"` // of the LAST run; empty once a run succeeds
	Runs      int        `json:"runs"`
	Errors    int        `json:"errors"`           // runs that returned an error, since boot
	Syncer    bool       `json:"syncer,omitempty"` // registered with Sync: its runs go to Done
}

// ErrSkip: a syncer with nothing to talk to (no credential stored) returns
// this. The run counts as a clean tick but is not a sync, so Done is not told.
var ErrSkip = errors.New("clock: skipped")

type task struct {
	Task
	every time.Duration
	fn    func() error
}

// Clock holds the registered tasks. Zero value is not usable: use New.
type Clock struct {
	mu    sync.Mutex
	tasks []*task
	wg    sync.WaitGroup
	// Now is the clock's idea of the time; tests replace it.
	Now func() time.Time
	// Tick is how often Start wakes; tasks are due at this resolution.
	Tick time.Duration
	// Last and Ran persist each task's last start across restarts (main wires
	// them to the settings table). Without them a not-at-boot task was first
	// due one interval after boot, and sessions restart the hub every hour or
	// two: the 6h SimpleFin, audience and prices syncs went 17 h without a
	// run on 2026-09-13 while /sources still said "every 6h".
	Last func(name string) time.Time
	Ran  func(name string, at time.Time)
	// Done hears every finished run of a Sync task (main writes it to the
	// sync_runs log, which Sources health reads). Housekeeping tasks
	// registered with Every are not reported.
	Done func(name string, start, end time.Time, err error)
}

// New returns an empty clock ticking once a minute.
func New() *Clock {
	return &Clock{Now: time.Now, Tick: time.Minute}
}

// Every registers fn to run every d. atBoot makes it due on the first tick;
// otherwise the first run is one interval after its last recorded start (Last),
// or after registration when it has none. A task's error
// is logged and kept on its row; it does not stop the task.
func (c *Clock) Every(name string, d time.Duration, atBoot bool, fn func() error) {
	c.add(name, d, atBoot, false, fn)
}

// Sync registers a syncer: a task that pulls from an outside service (or a
// file someone else writes). Same schedule as Every; every run that is not
// ErrSkip is reported to Done.
func (c *Clock) Sync(name string, d time.Duration, atBoot bool, fn func() error) {
	c.add(name, d, atBoot, true, fn)
}

func (c *Clock) add(name string, d time.Duration, atBoot, syncer bool, fn func() error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	next := now.Add(d)
	if atBoot {
		next = now
	} else if c.Last != nil {
		if last := c.Last(name); last.IsZero() {
			next = now // never recorded: it may never have run at all
		} else if last.Add(d).Before(next) {
			next = last.Add(d)
		}
	}
	c.tasks = append(c.tasks, &task{
		Task:  Task{Name: name, Every: human(d), NextDue: next, Syncer: syncer},
		every: d,
		fn:    fn,
	})
}

// Start runs the ticker until the process ends. The first tick is immediate,
// so at-boot tasks start right away.
func (c *Clock) Start() {
	go func() {
		for {
			c.TickAt(c.Now())
			time.Sleep(c.Tick)
		}
	}()
}

// TickAt starts every task due at now and returns how many it started. Each
// starts in its own goroutine; Wait blocks until the started ones finish.
func (c *Clock) TickAt(now time.Time) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	started := 0
	for _, t := range c.tasks {
		if t.Running || now.Before(t.NextDue) {
			continue
		}
		t.Running = true
		start := now
		t.LastStart = &start
		t.NextDue = now.Add(t.every)
		t.Runs++
		started++
		c.wg.Add(1)
		go c.run(t, start)
	}
	return started
}

func (c *Clock) run(t *task, start time.Time) {
	defer c.wg.Done()
	if c.Ran != nil {
		c.Ran(t.Name, start)
	}
	err := t.fn()
	skipped := errors.Is(err, ErrSkip)
	if skipped {
		err = nil
	}
	c.mu.Lock()
	end := c.Now()
	t.LastEnd = &end
	t.Running = false
	t.LastError = ""
	if err != nil {
		t.LastError = err.Error()
		t.Errors++
		log.Printf("clock: %s: %v", t.Name, err)
	}
	report := t.Syncer && !skipped && c.Done != nil
	c.mu.Unlock()
	if report {
		c.Done(t.Name, start, end, err)
	}
}

// Wait blocks until every task started so far has finished (tests).
func (c *Clock) Wait() { c.wg.Wait() }

// Status is the table /status carries under `clock`, in registration order.
func (c *Clock) Status() []Task {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Task, 0, len(c.tasks))
	for _, t := range c.tasks {
		out = append(out, t.Task)
	}
	return out
}

func human(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	case d%time.Hour == 0:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d%time.Minute == 0:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return d.String()
}

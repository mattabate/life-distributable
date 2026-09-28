package spend

import (
	"sort"
	"strconv"
	"strings"
)

// Model policy: which
// work runs on which model, at what effort, with what caps — a data table in
// ops/hub.json (`model_policy`), not code. The ladder stays what it was, a
// limit-avoidance device; the policy decides where on the ladder a wake
// STARTS. Measured before this existed: check-ins were 15% of the bill at
// fable/opus prices, jobs passed no --model at all, and a single wake could
// make 44 full-context calls.
//
// Two dimensions:
//   - trigger: what woke the process — `message` (the owner, or a hub relay),
//     `checkin` (a schedule), `decision` (approve/deny from the app), `job`
//     (ops/schedule.json), `prompt` (a headless prompt from the app).
//   - class: what the thread is for — `judgment` (design forks, reviews,
//     ambiguous finance: ladder from the top) or `build` (runbook execution,
//     sweeps, shipping: ladder from opus). A thread's class is set by hand
//     (`lifectl thread <id> model build`), else by its goal, else the default.
//     A model id is a class too: `claude-sonnet-5` pins a rote session
//     (an export, a diff, a count, copy) to sonnet for every wake.
//
// Only `job` (ops/schedule.json one-shots) names sonnet outright; every
// session wake — the owner's message, a decision, a check-in — starts on the
// session's own class rung (see the checkin rule).

// Rule: how one kind of wake runs.
type Rule struct {
	// Model: a model id, or "class" (= the thread's class model, then the
	// ladder from there), or "ladder" (top rung). "" = "class".
	Model string `json:"model"`
	// Effort: the CLI's --effort (low|medium|high|xhigh|max); "" = CLI default.
	Effort string `json:"effort"`
	// There is no tool-call limit here, by decision: a step cap cuts deep
	// work mid-pass. A turn ends when the session ends it. The remaining guards are the model, the
	// effort, and MaxBudgetUSD on unattended wakes — never a step count.
	//
	// MaxBudgetUSD: the CLI's own --max-budget-usd for the process. 0 = none.
	MaxBudgetUSD float64 `json:"max_budget_usd"`
	// Fresh: a LEAN wake — start a new claude process with no `--resume`, so
	// the turn pays for its prompt instead of the whole transcript again
	// (a scheduled thread whose every wake resumes a huge conversation pays
	// for all of it in cache reads, every time).
	// The hub keeps the thread and its history either way; the wake gets the
	// system preamble, the thread's goal notes and a recap of the last few
	// turns. Only sensible for scheduled wakes — never for the owner's messages.
	Fresh bool `json:"fresh"`
}

type Policy struct {
	Triggers map[string]Rule `json:"triggers"`
	// Classes: class name → the ladder rung its threads start on.
	Classes map[string]string `json:"classes"`
	// ClassByGoal: goal id → class, for threads with no class of their own.
	ClassByGoal  map[string]string `json:"class_by_goal"`
	DefaultClass string            `json:"default_class"`
	// DailyBudgetUSD: the 80/100/120 points ladder (budget package); 0 = off.
	DailyBudgetUSD float64 `json:"daily_budget_usd"`
	// ThreadDailyCapUSD: a scheduled thread that has spent this much today
	// is not woken again until tomorrow; 0 = off.
	ThreadDailyCapUSD float64 `json:"thread_daily_cap_usd"`
	// FableDailyUSD: the day's fable allowance. Past it
	// every new process starts one rung down (opus) for the rest of the
	// Eastern day; nothing pauses. 0 = off. See internal/budget.
	FableDailyUSD float64 `json:"fable_daily_usd"`
}

// Resolved: what a process is launched with.
type Resolved struct {
	// Model: the rung to start the ladder from ("" = top). The picker may
	// still step down from it when that rung's bucket is full.
	Model        string
	Effort       string
	MaxBudgetUSD float64
	Fresh        bool
}

// DefaultPolicy is what runs when ops/hub.json has no `model_policy`.
func DefaultPolicy() *Policy {
	return &Policy{
		Triggers: map[string]Rule{
			"message":  {Model: "class"},
			"decision": {Model: "class"},
			// A check-in runs on the SESSION'S OWN rung: a code review woken on
			// a cheap model is not worth reading. Sonnet is for copy and rote
			// checks, and only when a session is pinned to it (`lifectl thread
			// <id> model claude-sonnet-5`). Lean (no --resume) and $8-capped.
			"checkin": {Model: "class", MaxBudgetUSD: 8, Fresh: true},
			"job":     {Model: "claude-sonnet-5", Effort: "medium", MaxBudgetUSD: 5},
			"prompt":  {Model: "ladder"},
		},
		Classes:      map[string]string{"judgment": "", "build": "claude-opus-5-5"},
		ClassByGoal:  map[string]string{},
		DefaultClass: "judgment",
	}
}

// Merge fills what a configured policy leaves out from the default, so a
// hub.json that names two triggers does not silently drop the others.
func (p *Policy) Merge(def *Policy) *Policy {
	if p == nil {
		return def
	}
	if p.Triggers == nil {
		p.Triggers = map[string]Rule{}
	}
	for k, v := range def.Triggers {
		if _, ok := p.Triggers[k]; !ok {
			p.Triggers[k] = v
		}
	}
	if p.Classes == nil {
		p.Classes = map[string]string{}
	}
	for k, v := range def.Classes {
		if _, ok := p.Classes[k]; !ok {
			p.Classes[k] = v
		}
	}
	if p.ClassByGoal == nil {
		p.ClassByGoal = map[string]string{}
	}
	if p.DefaultClass == "" {
		p.DefaultClass = def.DefaultClass
	}
	return p
}

// Class of a thread: its own setting, else its goal's, else the default. A
// setting that is itself a model id ("claude-haiku-4-5-…") is returned as is.
func (p *Policy) Class(threadClass, goalID string) string {
	if threadClass != "" && threadClass != "auto" {
		return threadClass
	}
	if c, ok := p.ClassByGoal[goalID]; ok && goalID != "" {
		return c
	}
	return p.DefaultClass
}

// ClassModel: the rung a class starts on ("" = top of the ladder).
func (p *Policy) ClassModel(class string) string {
	if m, ok := p.Classes[class]; ok {
		return m
	}
	if strings.HasPrefix(class, "claude-") {
		return class // an explicit model id in place of a class
	}
	return ""
}

// Resolve: the rule for a wake of `trigger` on a thread of `threadClass`
// (with goal `goalID`). Unknown triggers get the `message` rule.
func (p *Policy) Resolve(trigger, threadClass, goalID string) Resolved {
	if p == nil {
		return Resolved{}
	}
	r, ok := p.Triggers[trigger]
	if !ok {
		r = p.Triggers["message"]
	}
	out := Resolved{Effort: r.Effort, MaxBudgetUSD: r.MaxBudgetUSD, Fresh: r.Fresh}
	switch r.Model {
	case "", "class":
		out.Model = p.ClassModel(p.Class(threadClass, goalID))
	case "ladder":
		out.Model = ""
	default:
		out.Model = r.Model
	}
	return out
}

// ClassNames, sorted, for messages and usage lines.
func (p *Policy) ClassNames() []string {
	var out []string
	for k := range p.Classes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Args: the CLI flags a resolved rule adds to a launch. No --max-turns, on
// any launch: nothing limits how many steps a turn may take (2026-08-28).
// oneShot is kept because a `claude -p` and a live session may yet need
// different flags; today they take the same ones.
func (r Resolved) Args(oneShot bool) []string {
	var args []string
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}
	if r.Effort != "" {
		args = append(args, "--effort", r.Effort)
	}
	if r.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(r.MaxBudgetUSD, 'f', 2, 64))
	}
	_ = oneShot
	return args
}

// ValidClass: what `lifectl thread <id> model …` accepts.
func (p *Policy) ValidClass(c string) bool {
	if c == "" || c == "auto" || strings.HasPrefix(c, "claude-") {
		return true
	}
	_, ok := p.Classes[c]
	return ok
}

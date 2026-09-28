// Package deliver is the ONE way a hub-side event reaches an agent: hand it
// to a session. A rec decision, an approved proposal from a scheduled job and
// a calendar agent item all used to open sessions with three private copies
// of the same rule (docs/reviews/2026-08-26-structure.md §1). The rule:
//
//   - relay into the session named, if it still exists and is not archived —
//     it already knows the argument;
//   - otherwise start a fresh session from the starter, which must therefore
//     carry the whole brief.
//
// The caller records which happened ("source" | "new") and the session id.
package deliver

import "life/hub/internal/threads"

type Deliverer struct{ Thr *threads.Manager }

// Target: where a message should go and what to send in each case.
type Target struct {
	// ThreadID: the session to relay into. "" always starts a new one.
	ThreadID string
	// Title/Project/GoalID: for the new session, when one is started.
	// Title "" lets the hub title it from the first reply.
	Title, Project, GoalID string
	// Relay: the message for the existing session. Starter: the full brief
	// for a fresh one.
	Relay, Starter string
	// Attachments: blob refs that ride with the message either way (a
	// screenshot attached to a rec decision).
	Attachments []string
}

// ToSession sends the message and returns the session it reached and how
// ("source" = relayed into t.ThreadID, "new" = a session it started). Which
// session is threads.Reach("new-or:<id>") — the same meaning a prompt's
// target has; a relay the session refuses falls through to a new one too.
func (d Deliverer) ToSession(t Target) (threadID, how string, err error) {
	target := "new"
	if t.ThreadID != "" {
		target = "new-or:" + t.ThreadID
	}
	if id, _ := d.Thr.Reach(target); id != "" {
		if err := d.Thr.SendAttached(id, t.Relay, t.Attachments); err == nil {
			return id, "source", nil
		}
	}
	proj := t.Project
	if proj == "" {
		proj = "life"
	}
	th, err := d.Thr.Create(t.Title, proj, t.GoalID, t.Starter, "", "", t.Attachments)
	if err != nil {
		return "", "", err
	}
	return th.ID, "new", nil
}

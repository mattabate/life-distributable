package recs

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"life/hub/internal/format"
	"life/hub/internal/store"
)

// The words both surfaces print on a rec, computed once (parity, 2026-09-14:
// the console said "$12 one-off" and "act by Jan 15 — stale" while the phone
// said "$11.50" and "stale since Jan 15" for the same row). They are added on
// the wire only — never columns, never read back from a request body.

// CostLabel is what saying yes costs, over the period it recurs: "$11/mo",
// "$120/yr", "$33,837 one-off", "free". A buy/subscribe filed at zero says
// "price not checked" rather than "free": an unpriced subscription is what
// makes the monthly total a lie (docs/design/recommendations.md rule 4).
func (r Rec) CostLabel() string {
	if r.CostCents <= 0 {
		if r.Kind == "buy" || r.Kind == "subscribe" {
			return "price not checked"
		}
		return "free"
	}
	amount := format.USD(float64(r.CostCents) / 100)
	switch r.CostPeriod {
	case "monthly":
		return amount + "/mo"
	case "yearly":
		return amount + "/yr"
	}
	return amount + " one-off"
}

// DaysLeft is Eastern calendar days from today to act_by (negative once it has
// passed); ok is false when there is no act_by.
func (r Rec) DaysLeft(now time.Time) (int, bool) {
	by, err := time.ParseInLocation("2006-01-02", r.ActBy, store.Eastern)
	if err != nil {
		return 0, false
	}
	n := now.In(store.Eastern)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, store.Eastern)
	return int(by.Sub(today).Round(24*time.Hour).Hours() / 24), true
}

// DatesLabel is the deadline and review line: "act by Jan 15 (12d) · review
// Oct 1", "act by Jan 15 — stale", "back Oct 1" while deferred.
func (r Rec) DatesLabel(now time.Time) string {
	var out []string
	if left, ok := r.DaysLeft(now); ok {
		if left < 0 {
			out = append(out, "act by "+shortDay(r.ActBy)+" — stale")
		} else {
			out = append(out, fmt.Sprintf("act by %s (%dd)", shortDay(r.ActBy), left))
		}
	}
	if r.ReviewOn != "" {
		word := "review"
		if r.Status == "deferred" {
			word = "back"
		}
		out = append(out, word+" "+shortDay(r.ReviewOn))
	}
	return strings.Join(out, " · ")
}

// MarshalJSON adds cost_label, days_left and dates_label to every rec served.
func (r Rec) MarshalJSON() ([]byte, error) {
	type alias Rec // no MarshalJSON, so this does not recurse
	now := time.Now()
	var left *int
	if n, ok := r.DaysLeft(now); ok {
		left = &n
	}
	return json.Marshal(struct {
		alias
		CostLabel  string `json:"cost_label"`
		DaysLeft   *int   `json:"days_left,omitempty"`
		DatesLabel string `json:"dates_label,omitempty"`
	}{alias(r), r.CostLabel(), left, r.DatesLabel(now)})
}

func shortDay(day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	return t.Format("Jan 2")
}

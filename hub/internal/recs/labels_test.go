package recs

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"life/hub/internal/store"
)

func TestWireLabels(t *testing.T) {
	now := time.Date(2026, 9, 14, 23, 30, 0, 0, store.Eastern) // late evening: UTC is already the 15th
	cases := []struct {
		r           Rec
		cost, dates string
	}{
		{Rec{Kind: "subscribe", CostCents: 1100, CostPeriod: "monthly"}, "$11/mo", ""},
		{Rec{Kind: "buy", CostCents: 3383700}, "$33,837 one-off", ""},
		{Rec{Kind: "buy", CostCents: 999, CostPeriod: "yearly", ActBy: "2026-09-15", ReviewOn: "2026-10-01"}, "$9.99/yr", "act by Sep 15 (1d) · review Oct 1"},
		{Rec{Kind: "subscribe"}, "price not checked", ""},
		{Rec{Kind: "habit", ActBy: "2026-09-13", Status: "deferred", ReviewOn: "2026-09-20"}, "free", "act by Sep 13 — stale · back Sep 20"},
	}
	for _, c := range cases {
		if got := c.r.CostLabel(); got != c.cost {
			t.Errorf("cost: got %q want %q", got, c.cost)
		}
		if got := c.r.DatesLabel(now); got != c.dates {
			t.Errorf("dates: got %q want %q", got, c.dates)
		}
	}
	b, _ := json.Marshal(Rec{ID: "rec-1", Kind: "buy", CostCents: 500, ActBy: "2099-01-01"})
	for _, want := range []string{`"id":"rec-1"`, `"cost_label":"$5 one-off"`, `"days_left":`, `"dates_label":"act by Jan 1 (`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("wire rec lacks %s: %s", want, b)
		}
	}
}

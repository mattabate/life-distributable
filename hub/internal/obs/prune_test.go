package obs

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"life/hub/internal/store"
)

// The one delete in this package touches only source=spend kind=quota rows
// older than the cut: a same-aged row of any other kind — and any other
// source with the same kind — is evidence and stays.
func TestPruneQuotaHeartbeatsTouchesOnlySpendQuota(t *testing.T) {
	dir := t.TempDir()
	db, _ := store.Open(filepath.Join(dir, "t.db"))
	s, err := New(db, filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	old, fresh := now.Add(-8*24*time.Hour), now.Add(-time.Hour)
	p := json.RawMessage(`{"key":"seven_day","percent":42}`)
	rows := []Observation{
		{Source: "spend", Kind: "quota", TS: old, Payload: p, UniqKey: "seven_day:old"},
		{Source: "spend", Kind: "quota", TS: old.Add(10 * time.Minute), Payload: p, UniqKey: "seven_day:old2"},
		{Source: "spend", Kind: "quota", TS: fresh, Payload: p, UniqKey: "seven_day:fresh"},
		{Source: "simplefin", Kind: "transaction", TS: old, Payload: json.RawMessage(`{"amount":-12.5}`)},
		{Source: "healthkit", Kind: "steps", TS: old, Payload: json.RawMessage(`{"n":9000}`)},
		{Source: "other", Kind: "quota", TS: old, Payload: p},   // same kind, not the hub's meter
		{Source: "spend", Kind: "summary", TS: old, Payload: p}, // same source, not the heartbeat
	}
	if _, _, err := s.InsertBatch(rows); err != nil {
		t.Fatal(err)
	}
	n, err := s.PruneQuotaHeartbeats(now.Add(-7 * 24 * time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("pruned %d, err %v", n, err)
	}
	left, _ := s.List(Query{Limit: 100})
	if len(left) != 5 {
		t.Fatalf("%d rows left, want 5", len(left))
	}
	quota, _ := s.List(Query{Source: "spend", Kind: "quota"})
	if len(quota) != 1 || !quota[0].TS.Equal(fresh) {
		t.Fatal(quota)
	}
	for _, o := range left {
		if o.Source == "spend" && o.Kind == "quota" && o.TS.Before(now.Add(-7*24*time.Hour)) {
			t.Fatal("an old heartbeat survived")
		}
	}
	// A second pass with nothing to do is a no-op, not an error.
	if n, err := s.PruneQuotaHeartbeats(now.Add(-7 * 24 * time.Hour)); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

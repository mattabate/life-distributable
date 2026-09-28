package syncruns

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"life/hub/internal/store"
)

func TestHealthNamesTheFailingStreak(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }
	l.Record("mail", at(0), at(0).Add(time.Minute), errors.New("old"))
	l.Record("mail", at(6), at(6).Add(time.Minute), nil)
	l.Record("mail", at(12), at(12).Add(time.Minute), errors.New("401"))
	l.Record("mail", at(18), at(18).Add(time.Minute), errors.New("403"))
	l.Record("gcal", at(1), at(1).Add(time.Second), nil)

	h, err := l.Health()
	if err != nil {
		t.Fatal(err)
	}
	m := h["mail"]
	if m.Fails != 2 || m.Error != "403" || !m.Since.Equal(at(12)) ||
		!m.LastOK.Equal(at(6).Add(time.Minute)) || !m.LastRun.Equal(at(18).Add(time.Minute)) {
		t.Fatalf("mail = %+v", m)
	}
	g := h["gcal"]
	if g.Fails != 0 || g.Error != "" || !g.LastOK.Equal(at(1).Add(time.Second)) {
		t.Fatalf("gcal = %+v", g)
	}
	if _, ok := h["device"]; ok {
		t.Fatal("a task that never ran has no health")
	}
}

package usage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"life/hub/internal/goals"
	"life/hub/internal/spend"
	"life/hub/internal/store"
	"life/hub/internal/threads"
)

func newDB(t *testing.T) *store.DB {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate("threads", threads.Schema); err != nil {
		t.Fatal(err)
	}
	if _, err := goals.New(db); err != nil {
		t.Fatal(err)
	}
	return db
}

// Off by default and after the owner says no: nothing leaves the Mac.
func TestOffSendsNothing(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()
	h := &Heartbeat{DB: newDB(t), Host: srv.URL}
	h.Seed(false)
	h.Seed(true) // the setup answer is recorded once; Settings owns it after
	if sent, err := h.Send(context.Background()); sent || err != nil || hit {
		t.Fatalf("sent=%v err=%v hit=%v, want nothing sent", sent, err, hit)
	}
}

// On: counts only — the body carries no message text, title or goal name.
func TestOnSendsCountsOnly(t *testing.T) {
	db := newDB(t)
	now := time.Now()
	for _, q := range []string{
		`INSERT INTO threads (id,created_at,updated_at,title,project) VALUES ('t1','` + store.TS(now) + `','` + store.TS(now) + `','Secret title','life')`,
		`INSERT INTO thread_messages (thread_id,ts,role,text) VALUES ('t1','` + store.TS(now) + `','owner','private words')`,
		`INSERT INTO goals (id,created_at,updated_at,title) VALUES ('g1','x','x','Private goal')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	var got map[string]any
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		json.Unmarshal(b, &got)
	}))
	defer srv.Close()
	h := &Heartbeat{DB: db, Host: srv.URL, Token: "phc_test", Pages: func() int { return 6 },
		Usages: func() ([]spend.Usage, error) { return []spend.Usage{{TS: now, Input: 2_000_000}}, nil }}
	h.Seed(true)
	if sent, err := h.Send(context.Background()); !sent || err != nil {
		t.Fatalf("sent=%v err=%v", sent, err)
	}
	for _, leak := range []string{"Secret", "private", "Private"} {
		if strings.Contains(raw, leak) {
			t.Errorf("heartbeat carries %q: %s", leak, raw)
		}
	}
	p := got["properties"].(map[string]any)
	if p["sessions_started"] != 1.0 || p["active_days"] != 1.0 || p["goals"] != 1.0 || p["pages"] != 6.0 || p["token_bucket"] != "1-10M" {
		t.Errorf("properties = %v", p)
	}
	if got["distinct_id"] != h.InstallID() || len(h.InstallID()) != 32 || h.LastSent() == "" {
		t.Errorf("id %v / %s, last_sent %q", got["distinct_id"], h.InstallID(), h.LastSent())
	}
}

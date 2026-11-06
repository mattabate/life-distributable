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
	if p["$geoip_disable"] != true || p["$ip"] != nil {
		t.Errorf("the sender's IP must not become a location: %v", p)
	}
	if got["distinct_id"] != h.InstallID() || len(h.InstallID()) != 32 || h.LastSent() == "" {
		t.Errorf("id %v / %s, last_sent %q", got["distinct_id"], h.InstallID(), h.LastSent())
	}
}

// Active days are the distinct days of the seven local days ending today,
// on the hub's own wall clock (store.Eastern): an 8pm message belongs to
// its local day even though UTC has rolled past midnight, and a message
// from the local day before the window does not sneak in through the
// rolling edge. Counting UTC dates the old way reports 7 here, not 6,
// and 8 when all seven days have a message (#120).
func TestActiveDaysOnTheHubClock(t *testing.T) {
	db := newDB(t)
	zone := store.Eastern
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, zone)
	at := func(month time.Month, day, hour int) string {
		return store.TS(time.Date(2026, month, day, hour, 0, 0, 0, zone))
	}
	msgs := []struct{ ts, role string }{
		{at(10, 29, 13), "owner"}, // the local day before the window
		{at(10, 30, 20), "owner"},
		{at(10, 31, 20), "owner"},
		{at(11, 1, 20), "owner"},
		{at(11, 2, 10), "claude"}, // not the owner: never an active day
		{at(11, 3, 20), "owner"},
		{at(11, 4, 20), "owner"},
		{at(11, 5, 20), "owner"}, // Nov 2 has no owner message on purpose
	}
	if _, err := db.Exec(`INSERT INTO threads (id,created_at,updated_at,title,project) VALUES ('t1','x','x','T','life')`); err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		q := `INSERT INTO thread_messages (thread_id,ts,role,text) VALUES ('t1','` + m.ts + `','` + m.role + `','x')`
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	h := &Heartbeat{DB: db, Now: func() time.Time { return now }}
	p, err := h.Payload()
	if err != nil {
		t.Fatal(err)
	}
	if p["active_days"] != 6 {
		t.Errorf("active_days = %v, want 6 (Oct 30, Oct 31, Nov 1, Nov 3, Nov 4, Nov 5 on the hub clock)", p["active_days"])
	}
}

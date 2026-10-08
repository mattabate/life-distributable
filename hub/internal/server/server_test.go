package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"life/hub/internal/actions"
	"life/hub/internal/budget"
	"life/hub/internal/calendar"
	"life/hub/internal/config"
	"life/hub/internal/goals"
	"life/hub/internal/obs"
	"life/hub/internal/recs"
	"life/hub/internal/sched"
	"life/hub/internal/store"
	"life/hub/internal/syncruns"
	"life/hub/internal/threads"
	"life/hub/internal/usage"
)

func newTest(t *testing.T) *Server {
	s, _ := newTestDB(t)
	return s
}

// newTestDB: the server on a fresh database, plus the database itself for
// tests that look at the schema or seed rows directly.
func newTestDB(t *testing.T) (*Server, *store.DB) {
	cfg := &config.Config{ListenAddr: "127.0.0.1:0", ClaudeProjectsDir: t.TempDir(), Root: t.TempDir(),
		Projects: []config.Project{{Name: "life", Dir: "/Users/owner/life"}}}
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	q := actions.New(db, nil, nil)
	sp := filepath.Join(t.TempDir(), "schedule.json")
	os.WriteFile(sp, []byte(`{"jobs":[{"name":"sweep","project":"life","when":"manual","prompt":"p"}]}`), 0o644)
	sc, err := sched.New(db, sp, q, "/bin/false", func(string) (string, bool) { return "/tmp", true })
	if err != nil {
		t.Fatal(err)
	}
	gs, _ := goals.New(db)
	ob, _ := obs.New(db, filepath.Join(t.TempDir(), "blobs"))
	th, _ := threads.New(db, "/usr/local/bin/claude", filepath.Join(t.TempDir(), "runs"), "life", func(p string) (string, bool) { return "/tmp", p == "life" }, nil, nil)
	th.Summarize = nil // no claude in tests
	th.Run = func(string, ...string) ([]byte, error) { return nil, nil }
	s := New(cfg, "secret", q, sc, gs, ob, th)
	s.Runs, _ = syncruns.New(db)
	rc, _ := recs.New(db)
	s.SetRecs(rc)
	s.Cal, _ = calendar.New(db, th) // the change feed reads items
	if g, err := budget.New(db, 100, 10, 250, "claude-opus-5"); err == nil {
		s.UseBudget(g)
	}
	// A fixed install id keeps shared/fixtures/usage.json stable.
	db.SetSetting("usage:install_id", "0123456789abcdef0123456789abcdef")
	s.Usage = &usage.Heartbeat{DB: db, Pages: ConsolePages}
	s.Usage.Seed(false)
	return s, db
}

// A fresh database, built by every package's Schema through store.Migrate,
// ends up with exactly these tables and indices — the live one's inventory.
// Adding a table or column: append to that package's Schema, then update the
// list here (hub/CLAUDE.md "Recipe: add a table/column").
func TestFreshSchema(t *testing.T) {
	_, db := newTestDB(t)
	inventory := func(kind string) string {
		rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type=? AND name NOT LIKE 'sqlite_%' ORDER BY name`, kind)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var n string
			rows.Scan(&n)
			names = append(names, n)
		}
		return strings.Join(names, " ")
	}
	wantTables := "action_events actions ask_events asks budget_days cal_events cal_items devices goal_notes goals item_events items observations prompts rec_events recs runs schema_migrations settings sync_runs thread_events thread_messages thread_runs threads voice_queue"
	wantIndices := "action_events_action actions_state asks_state asks_thread cal_events_item cal_items_day goal_notes_goal item_events_item items_ask items_day items_src items_thread obs_ingested_at obs_source_kind_ts obs_supersedes obs_uniq prompts_due prompts_target rec_events_rec recs_review recs_status runs_job sync_runs_name thread_events_thread thread_messages_run thread_messages_thread thread_runs_thread"
	if got := inventory("table"); got != wantTables {
		t.Errorf("tables:\n got %s\nwant %s", got, wantTables)
	}
	if got := inventory("index"); got != wantIndices {
		t.Errorf("indices:\n got %s\nwant %s", got, wantIndices)
	}
	// Running every migration a second time is a no-op: nothing new to apply.
	fresh, err := db.Migrate("threads", threads.Schema)
	if err != nil || len(fresh) != 0 {
		t.Fatalf("second migrate: fresh=%v err=%v", fresh, err)
	}
}

// do: authenticated request against the test server. body nil = no body.
func (s *Server) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, path, rdr)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

// The rec lifecycle end to end over HTTP: propose → decide → link → score →
// stats. /stats must not be swallowed by /{id}.
func TestRecsEndpoints(t *testing.T) {
	s := newTest(t)
	w := s.do(t, "POST", "/api/v1/recs", map[string]any{"title": "Try Semantic Scholar's API key",
		"domain": "audience", "kind": "try", "because": "Scholar alone gave 14 rows", "review_on": "2026-09-01"})
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var rec struct {
		ID, Status, Outcome, Links string
	}
	json.Unmarshal(w.Body.Bytes(), &rec)
	if rec.ID == "" || rec.Status != "proposed" {
		t.Fatalf("%+v", rec)
	}
	if w := s.do(t, "POST", "/api/v1/recs", map[string]any{"title": "bad", "domain": "sports"}); w.Code != 400 {
		t.Fatal("bad domain accepted", w.Code)
	}
	if w := s.do(t, "GET", "/api/v1/recs", nil); w.Code != 200 || !strings.Contains(w.Body.String(), rec.ID) {
		t.Fatal(w.Code, w.Body.String())
	}
	// /stats is a sibling of /{id} and must win the route match.
	w = s.do(t, "GET", "/api/v1/recs/stats", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"proposed":1`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := s.do(t, "GET", "/api/v1/recs/rec-zzzz", nil); w.Code != 404 {
		t.Fatal(w.Code)
	}
	w = s.do(t, "POST", "/api/v1/recs/"+rec.ID+"/decide", map[string]string{"status": "accepted", "note": "yes"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := s.do(t, "POST", "/api/v1/recs/"+rec.ID+"/decide", map[string]string{"status": "nonsense"}); w.Code != 409 {
		t.Fatal(w.Code)
	}
	// The owner's decision goes to an agent, and what it is told is composed here
	// so the phone and the console send the same thing. Their note is the opener —
	// it is the session title and the board preview.
	w = s.do(t, "GET", "/api/v1/recs/"+rec.ID+"/starter", nil)
	var st struct{ Opener, Context, Relay, RecID, SourceSession string }
	json.Unmarshal(w.Body.Bytes(), &st)
	if w.Code != 200 || st.Opener != "yes" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, want := range []string{rec.ID, "Scholar alone gave 14 rows", "carry it out", "2026-09-01", "lifectl rec " + rec.ID} {
		if !strings.Contains(st.Context, want) {
			t.Fatalf("starter context missing %q: %s", want, st.Context)
		}
	}
	// Nothing filed this one, so there is no session of its own to reply in.
	if st.SourceSession != "" {
		t.Fatal("source session out of nowhere:", st.SourceSession)
	}
	if !strings.Contains(st.Relay, "Accepted.") || !strings.Contains(st.Relay, "added none): yes") {
		t.Fatal("relay:", st.Relay)
	}
	if w := s.do(t, "GET", "/api/v1/recs/rec-zzzz/starter", nil); w.Code != 404 {
		t.Fatal(w.Code)
	}
	w = s.do(t, "POST", "/api/v1/recs/"+rec.ID+"/link", map[string]any{"refs": []string{"cal-1234", "cal-1234"}})
	json.Unmarshal(w.Body.Bytes(), &rec)
	if rec.Links != "cal-1234" {
		t.Fatal(rec.Links)
	}
	w = s.do(t, "POST", "/api/v1/recs/"+rec.ID+"/score", map[string]string{"outcome": "worked", "by": "claude", "note": "822 rows"})
	json.Unmarshal(w.Body.Bytes(), &rec)
	if w.Code != 200 || rec.Outcome != "worked" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = s.do(t, "GET", "/api/v1/recs/stats", nil)
	if !strings.Contains(w.Body.String(), `"hit_rate":1`) || !strings.Contains(w.Body.String(), `"accept_rate":1`) {
		t.Fatal(w.Body.String())
	}
}

// Deciding delivers: the owner's note is not filed away, it is handed to an
// agent — the session that filed the rec if it asked for that one, a new
// session otherwise, and a new session anyway when the old one is gone.
// Deferring is a date, not a verdict: the hub parks the rec, puts an agent
// check-in on the calendar that day in the session that filed it, and links
// the two. Delivery is optional and goes the same road as any decision.
func TestRecDeferMintsCheckIn(t *testing.T) {
	s := newTest(t)
	cdb, err := store.Open(filepath.Join(t.TempDir(), "cal.db"))
	if err != nil {
		t.Fatal(err)
	}
	cal, err := calendar.New(cdb, s.thr)
	if err != nil {
		t.Fatal(err)
	}
	s.Cal = cal
	w := s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": "Grow the audience"})
	var th threads.Thread
	json.Unmarshal(w.Body.Bytes(), &th)
	w = s.do(t, "POST", "/api/v1/recs", map[string]any{"title": "Switch on the X user token", "domain": "audience", "kind": "subscribe",
		"goal_id": "grow-my-audience", "thread_id": th.ID, "source": "claude:thread:" + th.ID, "model": "claude-opus-5"})
	var rec struct{ ID string }
	json.Unmarshal(w.Body.Bytes(), &rec)
	if w.Code != 201 || rec.ID == "" {
		t.Fatal(w.Code, w.Body.String())
	}

	// No date → 409, and nothing changes.
	if w = s.do(t, "POST", "/api/v1/recs/"+rec.ID+"/decide", map[string]string{"status": "deferred"}); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = s.do(t, "POST", "/api/v1/recs/"+rec.ID+"/decide", map[string]string{"status": "deferred", "until": "2099-09-23",
		"note": "not before the goals earn", "deliver": "source"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Status, Links, ReviewOn, Delivered, Err string
	}
	json.Unmarshal(w.Body.Bytes(), &struct {
		Status    *string `json:"status"`
		Links     *string `json:"links"`
		ReviewOn  *string `json:"review_on"`
		Delivered *string `json:"delivered"`
		Err       *string `json:"delivery_error"`
	}{&out.Status, &out.Links, &out.ReviewOn, &out.Delivered, &out.Err})
	if out.Status != "deferred" || out.ReviewOn != "2099-09-23" || out.Err != "" || out.Delivered != "source" {
		t.Fatalf("%+v", out)
	}
	if !strings.HasPrefix(out.Links, "cal-") {
		t.Fatal("no calendar check-in linked:", out.Links)
	}
	it, err := cal.Get(out.Links)
	if err != nil {
		t.Fatal(err)
	}
	// An agent run is never all day: a check-back minted with no
	// time gets the morning's first free slot.
	if it.Kind != "agent" || it.Day != "2099-09-23" || it.At != "08:00" || it.ThreadID != th.ID || it.GoalID != "grow-my-audience" ||
		!strings.Contains(it.Detail, rec.ID) || !strings.Contains(it.Detail, "not before the goals earn") {
		t.Fatalf("%+v", it)
	}
	// The session is told as a prompt answering the rec (the "Deferred until
	// …" frame is rendered from the row when it wakes).
	if msgs := s.do(t, "GET", "/api/v1/threads/"+th.ID+"/messages", nil).Body.String(); !strings.Contains(msgs, `"in_reply_to":"rec:`+rec.ID+`","outcome":"deferred"`) || !strings.Contains(msgs, "not before the goals earn") {
		t.Fatal("the filing session was not told:", msgs)
	}
	if h := s.thr.RecHeader(rec.ID, "deferred"); !strings.Contains(h, "Deferred until 2099-09-23") {
		t.Fatal("the frame the session reads:", h)
	}
	// From here on the brief carries what the rec points at, as it stands
	// now: the linked check-in, and the filing session by name.
	w = s.do(t, "GET", "/api/v1/recs/"+rec.ID+"/starter", nil)
	var st struct{ Context, Relay string }
	json.Unmarshal(w.Body.Bytes(), &st)
	for _, want := range []string{"What it points at:", "- " + it.ID + " · agent · 2099-09-23 08:00 · scheduled · Check back on:", "Filed from: session `" + th.ID + "`"} {
		if !strings.Contains(st.Context, want) {
			t.Fatalf("starter missing %q:\n%s", want, st.Context)
		}
	}
	if !strings.Contains(st.Relay, "- "+it.ID+" · agent") || strings.Contains(st.Relay, "Filed from") {
		t.Fatal("relay carries the links only:", st.Relay)
	}
	// Off the open list, in its own filter.
	if body := s.do(t, "GET", "/api/v1/recs", nil).Body.String(); strings.Contains(body, rec.ID) {
		t.Fatal("deferred rec still open")
	}
	if body := s.do(t, "GET", "/api/v1/recs?status=deferred", nil).Body.String(); !strings.Contains(body, rec.ID) {
		t.Fatal("deferred filter empty")
	}
}

func TestRecDecisionGoesToAnAgent(t *testing.T) {
	s := newTest(t)
	type decision struct {
		ID        string `json:"id"`
		Links     string `json:"links"`
		Delivered string `json:"delivered"`
		SessionID string `json:"session_id"`
		Err       string `json:"delivery_error"`
	}
	var out decision
	decide := func(recID, status, deliver, note string) {
		t.Helper()
		w := s.do(t, "POST", "/api/v1/recs/"+recID+"/decide",
			map[string]string{"status": status, "note": note, "deliver": deliver})
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		out = decision{}
		json.Unmarshal(w.Body.Bytes(), &out)
		if out.Err != "" {
			t.Fatal("nobody got it:", out.Err)
		}
	}
	newRec := func(title, threadID string) string {
		t.Helper()
		w := s.do(t, "POST", "/api/v1/recs", map[string]any{"title": title, "domain": "tools", "kind": "try",
			"thread_id": threadID, "source": "claude:thread:" + threadID, "model": "claude-opus-5"})
		var r struct{ ID string }
		json.Unmarshal(w.Body.Bytes(), &r)
		return r.ID
	}

	// A session filed it, so accepting replies there — no new session.
	w := s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": "Watch the puzzle page"})
	var th threads.Thread
	json.Unmarshal(w.Body.Bytes(), &th)
	id := newRec("Cache the read path", th.ID)
	decide(id, "accepted", "source", "already did it this morning")
	if out.Delivered != "source" || out.SessionID != th.ID {
		t.Fatalf("accept did not reach the session that filed it: %+v", out)
	}
	// It lands as a PROMPT answering the rec — the owner's words as the message,
	// the verdict as data — so the chat shows "↩ Accepted · <rec>" over the
	// note; the frame the agent reads is rendered at wake time.
	msgs := s.do(t, "GET", "/api/v1/threads/"+th.ID+"/messages", nil).Body.String()
	if !strings.Contains(msgs, "already did it this morning") || !strings.Contains(msgs, `"in_reply_to":"rec:`+id+`"`) || !strings.Contains(msgs, `"outcome":"accepted"`) {
		t.Fatal("the note never reached the session as an answer to the rec:", msgs)
	}
	if strings.Contains(msgs, "Accepted. Carry it out") {
		t.Fatal("the hub's frame is stored as if the owner typed it:", msgs)
	}
	// …and the session lists the rec, placed in its chat (message_id = the
	// first reply after it was filed; none yet here — no claude in tests).
	var inChat struct{ Recs []struct{ ID string } }
	json.Unmarshal(s.do(t, "GET", "/api/v1/recs?status=all&thread="+th.ID, nil).Body.Bytes(), &inChat)
	if len(inChat.Recs) != 1 || inChat.Recs[0].ID != id {
		t.Fatalf("GET /recs?thread= did not return the session's rec: %+v", inChat)
	}
	if body := s.do(t, "GET", "/api/v1/recs?status=all&thread=some-other-session", nil).Body.String(); strings.Contains(body, id) {
		t.Fatal("another session's list carries the rec:", body)
	}

	// Declining travels the same road — the reason is worth as much.
	id = newRec("Move to Firestore", th.ID)
	decide(id, "declined", "source", "not while it is free")
	if out.Delivered != "source" {
		t.Fatalf("%+v", out)
	}
	if msgs := s.do(t, "GET", "/api/v1/threads/"+th.ID+"/messages", nil).Body.String(); !strings.Contains(msgs, "not while it is free") {
		t.Fatal("decline never reached the session:", msgs)
	}

	// The owner can ask for a fresh session instead; it is linked to the rec.
	id = newRec("Try Phantombuster", th.ID)
	decide(id, "accepted", "new", "start clean on this one")
	if out.Delivered != "new" || out.SessionID == "" || out.SessionID == th.ID {
		t.Fatalf("%+v", out)
	}
	fresh := out.SessionID
	r := s.do(t, "GET", "/api/v1/recs/"+id, nil).Body.String()
	if !strings.Contains(r, fresh) {
		t.Fatal("the session it opened is not linked to the rec:", r)
	}

	// And a rec whose session is gone still gets the owner's words to somebody.
	id = newRec("Buy the third tranche", "long-gone-session")
	decide(id, "accepted", "source", "do it Thursday")
	if out.Delivered != "new" || out.SessionID == "" {
		t.Fatalf("a dead source session swallowed the note: %+v", out)
	}

	// The fourth button: a line without a verdict travels the same road and
	// leaves the rec exactly as open as it was.
	id = newRec("Raise the 401k deferral", th.ID)
	for _, bad := range []map[string]string{{"note": "", "deliver": "source"}, {"note": "which fund?", "deliver": ""}} {
		if w := s.do(t, "POST", "/api/v1/recs/"+id+"/reply", bad); w.Code != 409 {
			t.Fatalf("reply %v → %d %s", bad, w.Code, w.Body.String())
		}
	}
	w = s.do(t, "POST", "/api/v1/recs/"+id+"/reply", map[string]string{"note": "which fund would you put it in?", "deliver": "source"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	out = decision{}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Delivered != "source" || out.SessionID != th.ID {
		t.Fatalf("reply did not reach the filing session: %+v", out)
	}
	msgs = s.do(t, "GET", "/api/v1/threads/"+th.ID+"/messages", nil).Body.String()
	if !strings.Contains(msgs, "which fund would you put it in?") || !strings.Contains(msgs, `"in_reply_to":"rec:`+id+`"`) || strings.Contains(msgs, `"outcome":"accepted","text":"which fund`) {
		t.Fatal("the reply never reached the session as words about the rec:", msgs)
	}
	var still struct{ Status, Detail, DecisionNote string }
	json.Unmarshal(s.do(t, "GET", "/api/v1/recs/"+id, nil).Body.Bytes(), &still)
	if still.Status != "proposed" || still.DecisionNote != "" || !strings.Contains(still.Detail, "replied on") {
		t.Fatalf("a reply must not decide: %+v", still)
	}
	if body := s.do(t, "GET", "/api/v1/recs", nil).Body.String(); !strings.Contains(body, id) {
		t.Fatal("a replied-to rec left the open list")
	}
	if st := s.do(t, "GET", "/api/v1/recs/"+id+"/starter", nil).Body.String(); strings.Contains(st, `"source_session":"long-gone-session"`) {
		t.Fatal("offered a session that does not exist:", st)
	}

	// A screenshot rides IN the same message as the note, on either road —
	// never as a second message after it.
	s.thr.BlobPath = func(ref string) (string, error) {
		if ref != "sha256/ab/abc.png" {
			return "", errors.New("no such blob")
		}
		return "/data/blobs/" + ref, nil
	}
	id = newRec("Build a people layer", th.ID)
	w = s.do(t, "POST", "/api/v1/recs/"+id+"/decide", map[string]any{"status": "declined", "note": "already have this — see the screenshot",
		"deliver": "source", "attachments": []string{"sha256/ab/abc.png"}})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	msgs = s.do(t, "GET", "/api/v1/threads/"+th.ID+"/messages", nil).Body.String()
	var got []struct {
		Text        string
		Attachments []string
	}
	json.Unmarshal([]byte(msgs), &got)
	withPic := 0
	for _, m := range got {
		if len(m.Attachments) == 1 && m.Attachments[0] == "sha256/ab/abc.png" {
			withPic++
			if m.Text != "already have this — see the screenshot" {
				t.Fatal("the screenshot landed in a message of its own, not with the note:", msgs)
			}
		}
	}
	if withPic != 1 {
		t.Fatalf("the screenshot should ride in exactly one message, found %d: %s", withPic, msgs)
	}
	// A bad ref fails the delivery loudly; the decision itself stands.
	id = newRec("Try Notion", th.ID)
	w = s.do(t, "POST", "/api/v1/recs/"+id+"/decide", map[string]any{"status": "accepted", "deliver": "source", "attachments": []string{"nope"}})
	out = decision{}
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || !strings.Contains(out.Err, "no such blob") {
		t.Fatalf("a bad attachment should be reported: %d %+v", w.Code, out)
	}
	// A picture with no words is still a reply; the record says one was sent.
	id = newRec("Mirror the calendar", th.ID)
	w = s.do(t, "POST", "/api/v1/recs/"+id+"/reply", map[string]any{"deliver": "source", "attachments": []string{"sha256/ab/abc.png"}})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(s.do(t, "GET", "/api/v1/recs/"+id, nil).Body.Bytes(), &still)
	if !strings.Contains(still.Detail, "(sent 1 attachment)") {
		t.Fatalf("a photo-only reply left no line on the record: %+v", still)
	}
	// …and to a NEW session it is the first message's attachment.
	id = newRec("Ship the plugin", th.ID)
	w = s.do(t, "POST", "/api/v1/recs/"+id+"/reply", map[string]any{"deliver": "new", "note": "here is what I have", "attachments": []string{"sha256/ab/abc.png"}})
	out = decision{}
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out.Delivered != "new" || out.SessionID == "" {
		t.Fatalf("%d %+v", w.Code, out)
	}
	if msgs := s.do(t, "GET", "/api/v1/threads/"+out.SessionID+"/messages", nil).Body.String(); !strings.Contains(msgs, `"attachments":["sha256/ab/abc.png"]`) {
		t.Fatal("the new session's first message lost the screenshot:", msgs)
	}
}

// A job run reads back parsed: the prose the model wrote, the envelope's
// findings and needs_you — what a job's proposal opens on (no session).
func TestGetRun(t *testing.T) {
	s, db := newTestDB(t)
	raw := `{"is_error":false,"result":"Sweep done. One thing needs you.\n\n{\"summary\":\"all quiet\",\"findings\":[{\"kind\":\"commit\",\"title\":\"commit x\",\"detail\":\"why\"}],\"needs_you\":[\"renew passport\"]}","total_cost_usd":0.5}`
	if _, err := db.Exec(`INSERT INTO runs (id, job, started_at, finished_at, ok, summary, output, cost_usd) VALUES (1,'sweep','2026-08-26T11:30:00Z','2026-08-26T11:33:00Z',1,'all quiet',?,0.5)`, raw); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO runs (id, job, started_at, finished_at, ok, error, output) VALUES (2,'sweep','2026-08-26T12:30:00Z','2026-08-26T12:31:00Z',0,'claude: exit 1','not json')`)
	get := func(id string) (int, string) {
		r := httptest.NewRequest("GET", "/api/v1/runs/"+id, nil)
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	code, body := get("1")
	for _, want := range []string{`"text":"Sweep done. One thing needs you."`, `"findings":[{"kind":"commit","title":"commit x","detail":"why"`, `"needs_you":["renew passport"]`, `"summary":"all quiet"`, `"job":"sweep"`} {
		if code != 200 || !strings.Contains(body, want) {
			t.Fatal(code, want, body)
		}
	}
	code, body = get("2")
	if code != 200 || !strings.Contains(body, `"findings":[]`) || !strings.Contains(body, `"needs_you":[]`) || !strings.Contains(body, `"error":"claude: exit 1"`) || strings.Contains(body, `"text"`) {
		t.Fatal(code, body)
	}
	if code, _ := get("9"); code != 404 {
		t.Fatal(code)
	}
	if code, _ := get("x"); code != 400 {
		t.Fatal(code)
	}
}

// The phone's word on a push is what marks its session "speaking" on the
// board, not the push: `spoke` marks the session for the line's length, `stopped` and
// `silent` end it. The push itself marks nothing for the phone (notify).
func TestPhoneSpeechReportMarksSpeaking(t *testing.T) {
	s := newTest(t)
	post := func(body string) {
		r := httptest.NewRequest("POST", "/api/v1/observations", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	post(`{"source":"app","kind":"speech","payload":{"outcome":"spoke","route":"Headphones","state":"background","line":"Hey Alex, the build is out and the board is right.","thread":"th-voice"}}`)
	if !s.thr.Voice.Is("th-voice", time.Now()) {
		t.Fatal("a spoken report must mark the session speaking")
	}
	post(`{"source":"app","kind":"speech","payload":{"outcome":"stopped","route":"Speaker (Speaker)","state":"background","line":"Hey Alex","thread":"th-voice"}}`)
	if s.thr.Voice.Is("th-voice", time.Now()) {
		t.Fatal("a stopped report must end the mark")
	}
	// The line's real end beats the reckoned length.
	post(`{"source":"app","kind":"speech","payload":{"outcome":"spoke","route":"Headphones","state":"foreground","line":"Hey Alex, a line long enough to be marked for a good while yet.","thread":"th-voice"}}`)
	post(`{"source":"app","kind":"speech","payload":{"outcome":"finished","route":"Headphones","state":"foreground","line":"Hey Alex, a line long enough to be marked for a good while yet.","thread":"th-voice"}}`)
	if s.thr.Voice.Is("th-voice", time.Now()) {
		t.Fatal("a finished report must end the mark")
	}
	post(`{"source":"app","kind":"speech","payload":{"outcome":"silent","route":"Speaker (Speaker)","state":"foreground","line":"Hey Alex","thread":"th-quiet"}}`)
	if s.thr.Voice.Is("th-quiet", time.Now()) {
		t.Fatal("a silent report marks nothing")
	}
	// The batch route is the same Ingest: its speech report marks too (it
	// used to run only the workouts rebuild).
	r := httptest.NewRequest("POST", "/api/v1/observations/batch", strings.NewReader(`{"source":"app","items":[{"kind":"speech","payload":{"outcome":"spoke","route":"Headphones","state":"background","line":"Hey Alex, from a batch.","thread":"th-batch"}}]}`))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 201 || !s.thr.Voice.Is("th-batch", time.Now()) {
		t.Fatal("a batched speech report must mark the session speaking", w.Code, w.Body.String())
	}
}

// Blob first, then refs; after_id pages forward; a bad bound is a 400, not
// a silently unbounded read.
func TestObservationsBlobFirstAndAfterID(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := do("POST", "/api/v1/observations", `{"source":"app","kind":"photo","blob_ref":"sha256/ab/nope.png"}`); w.Code != 400 {
		t.Fatal("a ref with no blob was stored:", w.Code, w.Body.String())
	}
	w := do("POST", "/api/v1/blobs?ext=png", "pixels")
	var b struct {
		Ref   string
		Bytes int
	}
	json.Unmarshal(w.Body.Bytes(), &b)
	if w.Code != 201 || !strings.HasPrefix(b.Ref, "sha256/") || b.Bytes != 6 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("POST", "/api/v1/blobs?ext=../x", "pixels"); w.Code != 400 {
		t.Fatal("bad ext accepted:", w.Code)
	}
	w = do("POST", "/api/v1/observations/batch", `{"source":"laptop","items":[
		{"kind":"screen","uniq_key":"mac:1","blob_ref":"`+b.Ref+`"},
		{"kind":"click","uniq_key":"mac:2","tz":"EDT"},
		{"kind":"marker","uniq_key":"mac:3"}]}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"inserted":3`) {
		t.Fatal(w.Code, w.Body.String())
	}
	var rows []obs.Observation
	json.Unmarshal(do("GET", "/api/v1/observations?source=laptop&limit=1", "").Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0].Kind != "marker" {
		t.Fatalf("newest first: %+v", rows)
	}
	json.Unmarshal(do("GET", "/api/v1/observations?source=laptop&limit=5&after_id="+strconv.FormatInt(rows[0].ID-2, 10), "").Body.Bytes(), &rows)
	if len(rows) != 2 || rows[0].Kind != "click" || rows[1].Kind != "marker" || rows[0].TZ != "America/New_York" {
		t.Fatalf("after_id: %+v", rows)
	}
	for _, q := range []string{"since=yesterday", "until=2026-13-01", "after_id=x"} {
		if w := do("GET", "/api/v1/observations?"+q, ""); w.Code != 400 {
			t.Fatal(q, w.Code)
		}
	}
}

func TestObservationsUpload(t *testing.T) {
	s := newTest(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("source", "app")
	mw.WriteField("kind", "photo")
	mw.WriteField("ts", "2026-08-20T12:00:00Z")
	mw.WriteField("payload", `{"note":"lunch"}`)
	fw, _ := mw.CreateFormFile("file", "IMG_1.jpeg")
	fw.Write([]byte("not really a jpeg"))
	mw.Close()
	r := httptest.NewRequest("POST", "/api/v1/observations", &buf)
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"blob_ref":"sha256/`) || !strings.Contains(w.Body.String(), `"note":"lunch"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	var o obs.Observation
	json.Unmarshal(w.Body.Bytes(), &o)
	r = httptest.NewRequest("GET", "/api/v1/blobs/"+o.BlobRef, nil)
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "not really a jpeg" {
		t.Fatal(w.Code, w.Body.String())
	}
	r = httptest.NewRequest("POST", "/api/v1/observations", strings.NewReader(`{"source":"app","kind":"note","payload":{"text":"hi"}}`))
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "/api/v1/observations/counts", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if strings.Count(w.Body.String(), `"n":1`) != 2 {
		t.Fatal(w.Body.String())
	}
}

func TestGoalsAndStatus(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := do("POST", "/api/v1/goals", `{"title":"Make more money","statement":"grow income"}`); w.Code != 201 || !strings.Contains(w.Body.String(), `"id":"make-more-money"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("PATCH", "/api/v1/goals/make-more-money", `{"status":"paused"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"paused"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("POST", "/api/v1/goals/make-more-money/notes", `{"kind":"review","text":"hi"}`); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("GET", "/api/v1/goals/make-more-money/notes", ""); !strings.Contains(w.Body.String(), `"author":"owner"`) {
		t.Fatal(w.Body.String())
	}
	// The digest is the goal's state summary: rewritten in place, stamped with
	// its own date, and kept OUT of the list so the first call of every session
	// stays small (2026-08-27).
	if w := do("PATCH", "/api/v1/goals/make-more-money", `{"digest":"state: one account"}`); w.Code != 200 ||
		!strings.Contains(w.Body.String(), `"digest":"state: one account"`) || !strings.Contains(w.Body.String(), `"digest_at":`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("PATCH", "/api/v1/goals/make-more-money", `{"digest":"state: two accounts"}`); w.Code != 200 ||
		strings.Contains(w.Body.String(), "one account") {
		t.Fatal("digest must be rewritten in place, not appended:", w.Body.String())
	}
	if w := do("GET", "/api/v1/goals/make-more-money", ""); !strings.Contains(w.Body.String(), `"digest":"state: two accounts"`) {
		t.Fatal(w.Body.String())
	}
	if w := do("GET", "/api/v1/goals", ""); strings.Contains(w.Body.String(), "two accounts") {
		t.Fatal("goal list must omit the digest:", w.Body.String())
	}
	if w := do("GET", "/api/v1/status", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"pending_actions":0`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestScheduleEndpoints(t *testing.T) {
	s := newTest(t)
	do := func(method, url string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, nil)
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := do("GET", "/api/v1/schedule"); w.Code != 200 || !strings.Contains(w.Body.String(), `"sweep"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("POST", "/api/v1/schedule/nope/run"); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestActionsFlow(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := do("POST", "/api/v1/actions", `{"kind":"money","title":"move $5","exec_type":"none"}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"state":"proposed"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	var a actions.Action
	json.Unmarshal(w.Body.Bytes(), &a)
	if w := do("GET", "/api/v1/actions?state=open", ""); !strings.Contains(w.Body.String(), a.ID) {
		t.Fatal(w.Body.String())
	}
	// thread= narrows to one session's proposals
	w = do("POST", "/api/v1/actions", `{"kind":"money","title":"move $6","exec_type":"none","thread_id":"other-thread-1"}`)
	var b actions.Action
	json.Unmarshal(w.Body.Bytes(), &b)
	if w := do("GET", "/api/v1/actions?state=open&thread=other-thread-1", ""); !strings.Contains(w.Body.String(), b.ID) || strings.Contains(w.Body.String(), a.ID) {
		t.Fatal(w.Body.String())
	}
	// The hub words the card's buttons: a session's proposal has Reply, a job's does not.
	// (Each carries its hint — store/close.go ActionOutcomes.)
	if !regexp.MustCompile(`"outcomes":\[\{"value":"approved","label":"Approve","hint":"[^"]+"\},\{"value":"denied","label":"Deny","hint":"[^"]+"\},\{"value":"","label":"Reply","hint":"[^"]+"\}\]`).MatchString(w.Body.String()) {
		t.Fatal(w.Body.String())
	}
	if w := do("GET", "/api/v1/actions/"+a.ID, ""); !regexp.MustCompile(`\{"value":"denied","label":"Deny","hint":"[^"]+"\}\]`).MatchString(w.Body.String()) || strings.Contains(w.Body.String(), "Reply") {
		t.Fatal(w.Body.String())
	}
	// Dismiss is the silent close (2026-09-18): off the open list, nothing
	// decided, and Reopen puts it back; a dismissed row cannot be approved.
	if w := do("POST", "/api/v1/actions/"+b.ID+"/dismiss", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"dismissed"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("GET", "/api/v1/actions?state=open", ""); strings.Contains(w.Body.String(), b.ID) {
		t.Fatal("dismissed must leave the open list", w.Body.String())
	}
	if w := do("POST", "/api/v1/actions/"+b.ID+"/approve", ""); w.Code != 409 {
		t.Fatal("approve after dismiss must 409, got", w.Code)
	}
	if w := do("POST", "/api/v1/actions/"+b.ID+"/reopen", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"proposed"`) || strings.Contains(w.Body.String(), `"decided_at"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("POST", "/api/v1/actions/"+b.ID+"/reopen", ""); w.Code != 409 {
		t.Fatal("reopen of a proposed row must 409, got", w.Code)
	}
	if evs, _ := s.acts.Events(b.ID); len(evs) != 3 || evs[1].Event != "dismissed" || evs[2].Event != "reopened" {
		t.Fatal(evs)
	}
	if w := do("POST", "/api/v1/actions/"+a.ID+"/deny", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"denied"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("POST", "/api/v1/actions/"+a.ID+"/approve", ""); w.Code != 409 {
		t.Fatal("approve after deny must 409, got", w.Code)
	}
	// thread-linked proposal: decision body {message} is stored as the note
	w = do("POST", "/api/v1/actions", `{"kind":"delete","title":"drop dupe","thread_id":"th-1"}`)
	json.Unmarshal(w.Body.Bytes(), &a)
	if a.ThreadID != "th-1" {
		t.Fatal(w.Body.String())
	}
	if w := do("POST", "/api/v1/actions/"+a.ID+"/approve", `{"message":"only the first one"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"note":"only the first one"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

// The hub token belongs to every session, so on its own it must not be able
// to approve a gated action once the owner has armed a decider secret.
func TestArmedDeciderBlocksTokenOnlyApproval(t *testing.T) {
	s := newTest(t)
	dir := t.TempDir()
	s.cfg.TokenFile = filepath.Join(dir, "hub.token")
	const secret = "ABCDE-FGHJK-MNPQR-STVWX"
	if err := actions.NewDecider(filepath.Join(dir, "decider.hash")).Arm(secret); err != nil {
		t.Fatal(err)
	}

	do := func(method, url, code string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(""))
		r.Header.Set("Authorization", "Bearer secret")
		if code != "" {
			r.Header.Set("X-Life-Decider", code)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	propose := func() actions.Action {
		r := httptest.NewRequest("POST", "/api/v1/actions", strings.NewReader(`{"kind":"money","title":"move $5","exec_type":"none"}`))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		var a actions.Action
		json.Unmarshal(w.Body.Bytes(), &a)
		return a
	}

	a := propose() // proposing is still just the hub token: only deciding is gated
	if a.ID == "" {
		t.Fatal("propose failed")
	}
	if w := do("POST", "/api/v1/actions/"+a.ID+"/approve", ""); w.Code != 403 {
		t.Fatal("approve with no code must 403, got", w.Code, w.Body.String())
	}
	if w := do("POST", "/api/v1/actions/"+a.ID+"/deny", "WRONG-WRONG-WRONG-WRONG"); w.Code != 403 {
		t.Fatal("deny with a wrong code must 403, got", w.Code, w.Body.String())
	}
	// Dismiss and Reopen run nothing and tell nobody, so the token alone does them.
	if w := do("POST", "/api/v1/actions/"+a.ID+"/dismiss", ""); w.Code != 200 {
		t.Fatal("dismiss needs no decider code, got", w.Code, w.Body.String())
	}
	if w := do("POST", "/api/v1/actions/"+a.ID+"/reopen", ""); w.Code != 200 {
		t.Fatal("reopen needs no decider code, got", w.Code, w.Body.String())
	}
	// Both refusals are on the action's record, where the owner can see them.
	evs, err := s.acts.Events(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	refused := 0
	for _, e := range evs {
		if e.Event == "refused" {
			refused++
		}
	}
	if refused != 2 {
		t.Fatalf("both refused attempts must be in the audit trail, got %d of %v", refused, evs)
	}
	if w := do("POST", "/api/v1/actions/"+a.ID+"/approve", secret); w.Code != 200 {
		t.Fatal("the real code must approve, got", w.Code, w.Body.String())
	}
}

// The owner's approve/deny rides on a prompt (the approval card's
// buttons arm the chat bar like every other card's, and the note is the
// message): an `action:<id>` reply with approved|denied decides the row from
// POST /prompts, gated by the decider code exactly as /approve is, and the
// prompt itself is the relay — the queue must not post a second one.
func TestPromptDecidesAction(t *testing.T) {
	s := newTest(t)
	s.thr.DecideAction = func(id, outcome, note, via string) error {
		return s.acts.DecideQuiet(id, outcome == "approved", via, note)
	}
	s.acts.OnDecided = func(a actions.Action, approved bool, note string) {
		t.Errorf("a decision on a prompt must not relay a second prompt (%s)", a.ID)
	}
	do := func(method, url, body, code string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		if code != "" {
			r.Header.Set("X-Life-Decider", code)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	var th struct{ ID string }
	json.Unmarshal(do("POST", "/api/v1/threads", `{"prompt":"Stage it."}`, "").Body.Bytes(), &th)
	propose := func() string {
		w := do("POST", "/api/v1/actions", `{"kind":"commit","title":"push the branch","exec_type":"none","thread_id":"`+th.ID+`"}`, "")
		var a actions.Action
		json.Unmarshal(w.Body.Bytes(), &a)
		if a.ID == "" {
			t.Fatal("propose failed:", w.Code, w.Body.String())
		}
		return a.ID
	}

	// Unarmed hub: the token alone decides, note and surface land on the row.
	a := propose()
	if w := do("POST", "/api/v1/prompts?via=web", `{"target":"`+th.ID+`","text":"go ahead","in_reply_to":"action:`+a+`","outcome":"approved"}`, ""); w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	got, _ := s.acts.Get(a)
	if got.State == "proposed" || got.DecidedVia != "web" || got.Note != "go ahead" {
		t.Fatalf("approve on a prompt must decide the row: %+v", got)
	}
	// Denied, with the reason as the note.
	d := propose()
	if w := do("POST", "/api/v1/prompts", `{"target":"`+th.ID+`","text":"not that branch","replies":[{"ref":"action:`+d+`","outcome":"denied"}]}`, ""); w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got, _ := s.acts.Get(d); got.State != "denied" || got.DecidedVia != "app" || got.Note != "not that branch" {
		t.Fatalf("deny on a prompt must deny the row: %+v", got)
	}
	// Words alone leave the proposal open.
	o := propose()
	if w := do("POST", "/api/v1/prompts", `{"target":"`+th.ID+`","text":"what does it change?","in_reply_to":"action:`+o+`"}`, ""); w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got, _ := s.acts.Get(o); got.State != "proposed" {
		t.Fatalf("a reply without a pick must not decide: %+v", got)
	}

	// Armed hub: the prompt needs the code like /approve does; a refusal is
	// on the action's record and nothing is queued.
	dir := t.TempDir()
	s.cfg.TokenFile = filepath.Join(dir, "hub.token")
	const secret = "ABCDE-FGHJK-MNPQR-STVWX"
	if err := actions.NewDecider(filepath.Join(dir, "decider.hash")).Arm(secret); err != nil {
		t.Fatal(err)
	}
	g := propose()
	body := `{"target":"` + th.ID + `","text":"yes","in_reply_to":"action:` + g + `","outcome":"approved"}`
	if w := do("POST", "/api/v1/prompts", body, ""); w.Code != 403 {
		t.Fatal("approve on a prompt with no code must 403, got", w.Code, w.Body.String())
	}
	if got, _ := s.acts.Get(g); got.State != "proposed" {
		t.Fatalf("a refused prompt must not decide: %+v", got)
	}
	evs, _ := s.acts.Events(g)
	refused := 0
	for _, e := range evs {
		if e.Event == "refused" {
			refused++
		}
	}
	if refused != 1 {
		t.Fatalf("the refusal must be in the audit trail, got %v", evs)
	}
	if w := do("GET", "/api/v1/prompts?thread="+th.ID, "", ""); strings.Contains(w.Body.String(), `"text":"yes"`) {
		t.Fatal("a refused prompt must not be queued:", w.Body.String())
	}
	if w := do("POST", "/api/v1/prompts", body, secret); w.Code != 201 {
		t.Fatal("the real code must approve, got", w.Code, w.Body.String())
	}
	if got, _ := s.acts.Get(g); got.State == "proposed" {
		t.Fatalf("the real code must decide: %+v", got)
	}
}

// A surface must be able to find out its code is wrong BEFORE an approval
// rides on it: a phone holding a bad code re-sends it silently on every
// approve, which reads as the Approve button being broken.
func TestDeciderCheck(t *testing.T) {
	s := newTest(t)
	dir := t.TempDir()
	s.cfg.TokenFile = filepath.Join(dir, "hub.token")

	check := func(code string) map[string]bool {
		r := httptest.NewRequest("GET", "/api/v1/decider", nil)
		r.Header.Set("Authorization", "Bearer secret")
		if code != "" {
			r.Header.Set("X-Life-Decider", code)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var raw map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for k, v := range raw {
			if b, isBool := v.(bool); isBool {
				out[k] = b
			}
		}
		// set_at is the date a refusal is read against: there once a code
		// is armed, absent before.
		_, dated := raw["set_at"]
		if dated != out["armed"] {
			t.Fatalf("set_at present=%v but armed=%v: %s", dated, out["armed"], w.Body.String())
		}
		return out
	}

	// Unarmed: approve takes the token alone, so every surface is fine as-is.
	if got := check(""); got["armed"] || !got["ok"] {
		t.Fatal("unarmed must report armed=false ok=true, got", got)
	}

	const secret = "ABCDE-FGHJK-MNPQR-STVWX"
	if err := actions.NewDecider(filepath.Join(dir, "decider.hash")).Arm(secret); err != nil {
		t.Fatal(err)
	}
	if got := check(""); !got["armed"] || got["sent"] || got["ok"] {
		t.Fatal("armed with no code must report ok=false sent=false, got", got)
	}
	if got := check("WRONG-WRONG-WRONG-WRONG"); !got["sent"] || got["ok"] {
		t.Fatal("a wrong code must report sent=true ok=false, got", got)
	}
	if got := check(secret); !got["ok"] {
		t.Fatal("the real code must report ok=true, got", got)
	}
	// Checking is not deciding: a bad code here must NOT be recorded as a
	// refusal, or a phone polling its own settings would drown the evidence
	// that catches a session self-approving.
	r := httptest.NewRequest("POST", "/api/v1/actions", strings.NewReader(`{"kind":"money","title":"move $5","exec_type":"none"}`))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var a actions.Action
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil || a.ID == "" {
		t.Fatal("propose failed", w.Body.String())
	}
	check("WRONG-WRONG-WRONG-WRONG")
	evs, err := s.acts.Events(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Event == "refused" {
			t.Fatal("a failed check must not write a refusal event:", evs)
		}
	}
}

func TestAuth(t *testing.T) {
	s := newTest(t)
	for _, tc := range []struct {
		hdr  string
		want int
	}{{"", 401}, {"Bearer nope", 401}, {"Bearer secret", 200}} {
		r := httptest.NewRequest("GET", "/api/v1/projects", nil)
		if tc.hdr != "" {
			r.Header.Set("Authorization", tc.hdr)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%q: got %d want %d", tc.hdr, w.Code, tc.want)
		}
	}
	r := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("healthz must be unauthenticated")
	}
}

func TestIndexCookieFlow(t *testing.T) {
	s := newTest(t)
	r := httptest.NewRequest("GET", "/?token=secret", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 303 || !strings.Contains(w.Header().Get("Set-Cookie"), "life_token=secret") {
		t.Fatalf("%d %q", w.Code, w.Header().Get("Set-Cookie"))
	}
	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "life_token", Value: "secret"})
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<html") {
		t.Fatalf("%d", w.Code)
	}
}

func TestSpendAndSessions(t *testing.T) {
	s := newTest(t)
	p := filepath.Join(s.cfg.ClaudeProjectsDir, "x", "a.jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"type":"assistant","requestId":"r","sessionId":"s","cwd":"/Users/owner/life/hub","timestamp":"2099-01-01T00:00:00Z","message":{"id":"m","model":"claude-opus-5","usage":{"input_tokens":1000000,"output_tokens":0}}}`+"\n"), 0o644)
	get := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := get("GET", "/api/v1/spend/summary?days=100000", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"key":"life"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	// The Spend tab's 5-hour range: an hours window, not a day count.
	w = get("GET", "/api/v1/spend/summary?hours=5", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"window_hours":5`) ||
		!strings.Contains(w.Body.String(), `"days":0`) || !strings.Contains(w.Body.String(), `"key":"life"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	// The remote-control lane is gone (2026-09-30): nothing starts a
	// `claude remote-control` server from the hub any more.
	if w := get("POST", "/api/v1/sessions", `{"project":"life"}`); w.Code != 404 && w.Code != 405 {
		t.Fatal(w.Code, w.Body.String())
	}
}

// The Spend page's model toggle: what it sets is PINNED into the new thread's
// model_class so a conversation never changes model mid-chat (prompt caching).
func TestDefaultModelToggle(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := do("GET", "/api/v1/spend/model", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"explicit":false`) ||
		!strings.Contains(w.Body.String(), `"claude-fable-5-1","claude-opus-5-5","claude-sonnet-5-5"`) ||
		!strings.Contains(w.Body.String(), `"rungs":[{"model":"claude-fable-5-1","open":`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	// Nothing off the ladder.
	if w := do("PUT", "/api/v1/spend/model", `{"default_model":"claude-haiku-4-5"}`); w.Code != 400 {
		t.Fatalf("haiku accepted: %d %s", w.Code, w.Body.String())
	}
	if w := do("PUT", "/api/v1/spend/model", `{"default_model":"claude-fable-5-1"}`); w.Code != 200 ||
		!strings.Contains(w.Body.String(), `"explicit":true`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	first, err := s.thr.Create("Groceries", "life", "", "hi", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.ModelClass != "claude-fable-5-1" {
		t.Fatalf("new thread not pinned: %q", first.ModelClass)
	}
	// Flipping it later leaves the running conversation exactly where it was.
	if w := do("PUT", "/api/v1/spend/model", `{"default_model":"claude-opus-5-5"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	again, _ := s.thr.Get(first.ID)
	if again.ModelClass != "claude-fable-5-1" {
		t.Fatalf("an existing thread moved: %q", again.ModelClass)
	}
	next, _ := s.thr.Create("Taxes", "life", "", "hi", "", "", nil)
	if next.ModelClass != "claude-opus-5-5" {
		t.Fatalf("next thread not on the new default: %q", next.ModelClass)
	}
	// Cleared: back to the policy, and a thread made then carries no pin.
	if w := do("PUT", "/api/v1/spend/model", `{"default_model":""}`); w.Code != 200 ||
		!strings.Contains(w.Body.String(), `"explicit":false`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	auto, _ := s.thr.Create("Whatever", "life", "", "hi", "", "", nil)
	if auto.ModelClass != "" {
		t.Fatalf("cleared toggle still pinned: %q", auto.ModelClass)
	}
}

// TestContractCoverage: every route in shared/api.md must be registered and
// vice versa. The shapes are pinned separately by TestFixtures (real hub
// output in shared/fixtures, decoded by the app's FixtureDecodeTests).
func TestContractCoverage(t *testing.T) {
	b, err := os.ReadFile("../../../shared/api.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "### ") {
			doc[strings.TrimSpace(strings.TrimPrefix(line, "### "))] = true
		}
	}
	code := map[string]bool{}
	for _, r := range newTest(t).Routes() {
		code[r] = true
		if !doc[r] {
			t.Errorf("route %q registered but missing from shared/api.md", r)
		}
	}
	for r := range doc {
		if !code[r] {
			t.Errorf("route %q documented but not registered", r)
		}
	}
}

func TestThreadsAPI(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := do("POST", "/api/v1/threads", `{"prompt":"Split my brokerage cash sensibly","goal_id":"make-more-money"}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"status":"running"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	var th threads.Thread
	json.Unmarshal(w.Body.Bytes(), &th)
	// Messaging a working session steers the message into the running turn.
	if w := do("POST", "/api/v1/threads/"+th.ID+"/messages", `{"text":"and hurry"}`); w.Code != 202 {
		t.Fatal("send while running should be accepted (202), got", w.Code)
	}
	if w := do("GET", "/api/v1/threads/"+th.ID+"/messages", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"role":"owner"`) || !strings.Contains(w.Body.String(), `"steered":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("GET", "/api/v1/threads/"+th.ID+"/events?since=0", ""); w.Code != 200 || w.Body.String() != "[]\n" {
		t.Fatal(w.Code, w.Body.String())
	}
	// The page before the oldest held event is a cursor the console walks
	// back with; an empty thread answers it the same way.
	if w := do("GET", "/api/v1/threads/"+th.ID+"/events?before=5&limit=2", ""); w.Code != 200 || w.Body.String() != "[]\n" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("GET", "/api/v1/threads", ""); !strings.Contains(w.Body.String(), th.ID) {
		t.Fatal(w.Body.String())
	}
}

// Speed phase (2026-08-26): an unchanged JSON GET answers 304 to the ETag it
// handed out, errors and non-GETs carry no tag, and the change feed answers
// at once without `since`, waits (changed=false) while nothing moves, and
// returns the moment something does — for one thread or for everything.
// A streamed step moves the open chat's version but not the global one (the
// phone's root loop re-fetched ~1×/s while any session ran, 2026-09-14); a
// status flip that does not touch updated_at still moves the global one.
func TestGlobalVersionIgnoresSteps(t *testing.T) {
	s, db := newTestDB(t)
	th, err := s.thr.Create("", "life", "", "Watch the ledger.", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// no turn in flight, so the 15 s clock cannot move the global version
	db.Exec(`UPDATE thread_runs SET busy=0`)
	db.Exec(`UPDATE threads SET status='idle' WHERE id=?`, th.ID)
	g0, _ := s.thr.Version("")
	t0, _ := s.thr.Version(th.ID)
	if _, err := db.Exec(`INSERT INTO thread_events (thread_id, run_id, ts, kind, title, body, summary) VALUES (?,'r1','2026-09-14T00:00:00Z','tool_use','Bash','ls','')`, th.ID); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.thr.Version(""); g != g0 {
		t.Fatal("global version moved on a step")
	}
	if v, _ := s.thr.Version(th.ID); v == t0 {
		t.Fatal("thread version did not move on a step")
	}
	db.Exec(`UPDATE threads SET status='needs_you' WHERE id=?`, th.ID)
	if g, _ := s.thr.Version(""); g == g0 {
		t.Fatal("global version missed a status flip")
	}
}

func TestETagAndChanges(t *testing.T) {
	s := newTest(t)
	do := func(method, url, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(`{"prompt":"Watch the ledger."}`))
		r.Header.Set("Authorization", "Bearer secret")
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	first := do("GET", "/api/v1/threads", "")
	tag := first.Header().Get("ETag")
	if first.Code != 200 || tag == "" || first.Body.String() != "[]\n" {
		t.Fatal(first.Code, tag, first.Body.String())
	}
	if w := do("GET", "/api/v1/threads", tag); w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("ETag") != tag {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("GET", "/api/v1/threads/nope", ""); w.Code != 404 || w.Header().Get("ETag") != "" || !strings.Contains(w.Body.String(), "no such thread") {
		t.Fatal(w.Code, w.Header().Get("ETag"), w.Body.String())
	}
	if w := do("POST", "/api/v1/threads", ""); w.Code != 201 || w.Header().Get("ETag") != "" {
		t.Fatal(w.Code, w.Header().Get("ETag"))
	}
	var list []threads.Thread
	json.Unmarshal(do("GET", "/api/v1/threads", "").Body.Bytes(), &list)
	th := list[0]
	// the list changed, so the old tag no longer matches and a new one comes back
	if w := do("GET", "/api/v1/threads", tag); w.Code != 200 || w.Header().Get("ETag") == tag || !strings.Contains(w.Body.String(), th.ID) {
		t.Fatal(w.Code, w.Header().Get("ETag"))
	}

	changesTick = 10 * time.Millisecond
	defer func() { changesTick = time.Second }()
	threads.VersionTick = 1 << 62 // a running turn's clock must not move the version mid-wait
	defer func() { threads.VersionTick = 15 * time.Second }()
	var feed struct {
		Version string
		Changed bool
	}
	w := do("GET", "/api/v1/changes", "")
	json.Unmarshal(w.Body.Bytes(), &feed)
	if w.Code != 200 || feed.Version == "" || feed.Changed {
		t.Fatal(w.Code, w.Body.String())
	}
	v := feed.Version
	start := time.Now()
	w = do("GET", "/api/v1/changes?since="+v+"&wait=1", "")
	json.Unmarshal(w.Body.Bytes(), &feed)
	if feed.Changed || feed.Version != v || time.Since(start) < 900*time.Millisecond {
		t.Fatal(w.Body.String(), time.Since(start))
	}
	if w := do("GET", "/api/v1/changes?since="+v+"&thread=bad%20id", ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = do("GET", "/api/v1/changes?since=x&thread="+th.ID, "")
	json.Unmarshal(w.Body.Bytes(), &feed)
	tv := feed.Version
	if tv == "" || !feed.Changed {
		t.Fatal(w.Body.String())
	}
	// something happens in the background → both feeds return at once
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.thr.Send(th.ID, "and hurry")
	}()
	start = time.Now()
	w = do("GET", "/api/v1/changes?since="+v+"&wait=5", "")
	json.Unmarshal(w.Body.Bytes(), &feed)
	if !feed.Changed || feed.Version == v || time.Since(start) > 2*time.Second {
		t.Fatal(w.Body.String(), time.Since(start))
	}
	w = do("GET", "/api/v1/changes?since="+tv+"&thread="+th.ID+"&wait=5", "")
	json.Unmarshal(w.Body.Bytes(), &feed)
	if !feed.Changed || feed.Version == tv {
		t.Fatal(w.Body.String())
	}
	// a restart: a parked feed answers at once, so Shutdown is not held
	w = do("GET", "/api/v1/changes", "")
	json.Unmarshal(w.Body.Bytes(), &feed)
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.Draining()
		s.Draining() // idempotent
	}()
	start = time.Now()
	w = do("GET", "/api/v1/changes?since="+feed.Version+"&wait=5", "")
	if w.Code != 200 || time.Since(start) > 2*time.Second {
		t.Fatal(w.Code, w.Body.String(), time.Since(start))
	}
	// the list preview is a prefix, never the whole reply
	s.thr.Send(th.ID, strings.Repeat("x", 2000))
	if w := do("GET", "/api/v1/threads", ""); strings.Contains(w.Body.String(), strings.Repeat("x", 601)) || !strings.Contains(w.Body.String(), `"last_message":"xxx`) {
		t.Fatal("preview not trimmed", w.Body.String())
	}
}

// The heartbeat switch: off until the owner says yes, and the state shows
// exactly what a send would carry — counts, never a title.
func TestUsageSwitch(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	s.thr.CreateIdle("th-private", "A private title", "life")
	if w := do("GET", "/api/v1/usage", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"on":false`) || !strings.Contains(w.Body.String(), `"pages":4`) || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("PUT", "/api/v1/usage", `{}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("PUT", "/api/v1/usage", `{"on":true}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"on":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

// The daily budget meter and a thread's model class (2026-08-26): both are
// small wrappers, the rules live in budget/ and spend/policy.go.
func TestBudgetAndModelClass(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := do("GET", "/api/v1/spend/budget", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"level":"ok"`) || !strings.Contains(w.Body.String(), `"budget_usd":100`) || !strings.Contains(w.Body.String(), `"unattended":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	// under budget there is nothing to lift: the level stays ok, the call is recorded
	if w := do("POST", "/api/v1/spend/budget/clear", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"level":"ok"`) || !strings.Contains(w.Body.String(), `"cleared_at":"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("GET", "/api/v1/spend/summary?days=1", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"by_trigger"`) || !strings.Contains(w.Body.String(), `"jobs"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = do("POST", "/api/v1/threads", `{"prompt":"Ship the phase","goal_id":"make-more-money"}`)
	var th threads.Thread
	json.Unmarshal(w.Body.Bytes(), &th)
	if w := do("PATCH", "/api/v1/threads/"+th.ID, `{"model_class":"bogus"}`); w.Code != 400 || !strings.Contains(w.Body.String(), "build") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("PATCH", "/api/v1/threads/"+th.ID, `{"model_class":"build"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"model_class":"build"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("PATCH", "/api/v1/threads/"+th.ID, `{"model_class":"auto"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"model_class":""`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

// The Respond control on both surfaces posts one prompt: what it answers,
// the outcome, where it goes and when. The reference has to survive the wire
// (an untagged `in_reply_to` decoded to "" and the whole point was lost).
// An open card's words can be rewritten in place (`lifectl ask <id> set`),
// quietly, and a session can be found by what it knows (`lifectl threads
// --q`) — the two halves of cross-session care (2026-10-02).
func TestRewordAskAndFindThreads(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	var th struct{ ID string }
	json.Unmarshal(do("POST", "/api/v1/threads", `{"prompt":"Sort out the Lemurs brief.","title":"Face and supplements"}`).Body.Bytes(), &th)
	var a struct{ ID string }
	json.Unmarshal(do("POST", "/api/v1/asks", `{"thread_id":"`+th.ID+`","title":"Hand over the Lemurs AI email","kind":"physical"}`).Body.Bytes(), &a)

	w := do("POST", "/api/v1/asks/"+a.ID+"/reword", `{"title":"Forward the Lemurs email","say":"Hey, forward the Lemurs email.","by":"claude:thread:other"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"title":"Forward the Lemurs email"`) || !strings.Contains(w.Body.String(), `"said":"Hey, forward the Lemurs email."`) || !strings.Contains(w.Body.String(), `"state":"open"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := do("POST", "/api/v1/asks/"+a.ID+"/reword", `{}`); w.Code != 409 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := do("POST", "/api/v1/asks/ask-nope/reword", `{"title":"x"}`); w.Code != 404 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	do("POST", "/api/v1/threads", `{"prompt":"Draft today's tweet.","title":"Daily tweet draft"}`)
	var found []struct{ ID string }
	json.Unmarshal(do("GET", "/api/v1/threads?q=lemurs+supplements", "").Body.Bytes(), &found)
	if len(found) != 1 || found[0].ID != th.ID {
		t.Fatalf("%+v", found)
	}
	json.Unmarshal(do("GET", "/api/v1/threads", "").Body.Bytes(), &found)
	if len(found) != 2 {
		t.Fatalf("%+v", found)
	}
}

func TestPromptsAPI(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	var th struct{ ID string }
	json.Unmarshal(do("POST", "/api/v1/threads", `{"prompt":"Plan the tranches."}`).Body.Bytes(), &th)
	var a struct{ ID string }
	json.Unmarshal(do("POST", "/api/v1/asks", `{"thread_id":"`+th.ID+`","title":"Pick a rebalance date","kind":"decision"}`).Body.Bytes(), &a)

	// Done, with words, for tomorrow morning: the card closes now, the prompt waits.
	w := do("POST", "/api/v1/prompts", `{"target":"`+th.ID+`","text":"the 4th","in_reply_to":"ask:`+a.ID+`","outcome":"done","on":"2099-01-02","at_time":"09:00"}`)
	var p struct{ ID string }
	var raw map[string]any
	json.Unmarshal(w.Body.Bytes(), &raw)
	json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 201 || raw["in_reply_to"] != "ask:"+a.ID || raw["outcome"] != "done" || raw["state"] != "queued" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got := do("GET", "/api/v1/asks/"+a.ID, "").Body.String(); !strings.Contains(got, `"state":"done"`) || !strings.Contains(got, `"resolution":"the 4th"`) {
		t.Fatal(got)
	}
	// A session's own future, and taking it back.
	if got := do("GET", "/api/v1/prompts?state=queued&thread="+th.ID, "").Body.String(); !strings.Contains(got, p.ID) {
		t.Fatal(got)
	}
	if w := do("POST", "/api/v1/prompts/"+p.ID+"/cancel", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"cancelled"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := do("POST", "/api/v1/prompts/"+p.ID+"/cancel", ""); w.Code != 400 {
		t.Fatal("cancelling twice should fail:", w.Code)
	}
	// A reference the hub could not render is refused rather than becoming prose.
	if w := do("POST", "/api/v1/prompts", `{"target":"`+th.ID+`","text":"x","in_reply_to":"invoice:9"}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
}

// A session posting a card mid-turn without naming its run (LIFE_RUN_ID
// missing from the claude process) still gets tied to the turn it was raised
// in — that tie is the only thing that puts the card IN the tool chain and,
// at turn end, under the reply.
func TestAskWithoutRunIDTakesTheLiveRun(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	var th struct{ ID string }
	json.Unmarshal(do("POST", "/api/v1/threads", `{"prompt":"Plan the tranches."}`).Body.Bytes(), &th)
	run := s.thr.LiveRun(th.ID)
	if run == "" {
		t.Fatal("a fresh thread is running: it has a live run")
	}
	got := do("POST", "/api/v1/asks", `{"thread_id":"`+th.ID+`","title":"Which account?","kind":"decision"}`).Body.String()
	if !strings.Contains(got, `"run_id":"`+run+`"`) {
		t.Fatalf("card must take the live run: %s", got)
	}
}

// The per-ask web page is gone: the chat shows the whole
// card, so an ask carries no `url` and /a/… is not a route.
func TestNoAskPage(t *testing.T) {
	s := newTest(t)
	s.cfg.PublicHost = "hub.test:8443"
	s.cfg.BlobDir = filepath.Join(t.TempDir(), "blobs")
	ota := filepath.Join(filepath.Dir(s.cfg.BlobDir), "ota")
	os.MkdirAll(ota, 0o755)
	os.WriteFile(filepath.Join(ota, ".token"), []byte("sekret\n"), 0o600)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	do("POST", "/api/v1/threads", `{"prompt":"hello"}`)
	var th []struct{ ID string }
	json.Unmarshal(do("GET", "/api/v1/threads", "").Body.Bytes(), &th)
	if len(th) == 0 {
		t.Fatal("no thread")
	}
	w := do("POST", "/api/v1/asks", `{"thread_id":"`+th[0].ID+`","title":"Steps","kind":"read"}`)
	var a struct{ ID string }
	json.Unmarshal(w.Body.Bytes(), &a)
	if w.Code != 201 || strings.Contains(w.Body.String(), `"url"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	r := httptest.NewRequest("GET", "/a/sekret/"+a.ID, nil)
	pw := httptest.NewRecorder()
	s.ServeHTTP(pw, r)
	if strings.Contains(pw.Body.String(), "Steps") {
		t.Fatalf("ask page still served: %d %s", pw.Code, pw.Body)
	}
}

// A source whose syncer's newest run failed reads "failing" on Sources, from
// the one sync_runs log — the clock's ck.Sync hook writes it.
func TestSourcesReadSyncRuns(t *testing.T) {
	s := newTest(t)
	t0 := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	s.Runs.Record("feed", t0, t0.Add(time.Minute), nil)
	s.Runs.Record("feed", t0.Add(6*time.Hour), t0.Add(6*time.Hour+time.Minute), errors.New("feed: 401"))
	s.Runs.Record("other", t0, t0.Add(time.Minute), nil)
	runs, err := s.Runs.Health()
	if err != nil {
		t.Fatal(err)
	}
	feed := Source{ID: "feed", Status: "live"}
	applyRuns(&feed, runs)
	if feed.Status != "failing" || feed.Error != "feed: 401" || feed.Fails != 1 ||
		feed.LastOK == nil || !feed.LastOK.Equal(t0.Add(time.Minute)) || !feed.Since.Equal(t0.Add(6*time.Hour)) {
		t.Fatalf("feed = %+v", feed)
	}
	ok := Source{ID: "other", Status: "live"}
	applyRuns(&ok, runs)
	if ok.Status != "connected" || ok.Error != "" || ok.LastOK == nil {
		t.Fatalf("other = %+v", ok)
	}
	// No runs: the row keeps its own status.
	none := Source{ID: "health", Status: "live"}
	applyRuns(&none, runs)
	if none.Status != "live" || none.LastOK != nil {
		t.Fatalf("no runs = %+v", none)
	}
	if w := s.do(t, "GET", "/api/v1/sources", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

// The Sources page says what the hub actually holds: a
// catalogued source with rows shows, one with none is absent, and anything
// uncatalogued that landed rows is listed under Other.
func TestSources(t *testing.T) {
	s := newTest(t)
	for _, o := range []obs.Observation{
		{Source: "health", Kind: "steps", TS: time.Now(), Payload: json.RawMessage(`{}`)},
		{Source: "health", Kind: "steps", TS: time.Now().Add(-time.Hour), Payload: json.RawMessage(`{}`)},
		{Source: "garden", Kind: "reading", TS: time.Now(), Payload: json.RawMessage(`{}`)},
		{Source: "spend", Kind: "quota", TS: time.Now(), Payload: json.RawMessage(`{}`)},
	} {
		if _, err := s.obs.Insert(o); err != nil {
			t.Fatal(err)
		}
	}
	w := s.do(t, "GET", "/api/v1/sources", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	b := w.Body.String()
	// Each group carries its Configuration section (tag + colour) and each
	// source its tile: the catalogued health source its own heart, an
	// uncatalogued one its initial on slate.
	for _, want := range []string{`"title":"Apple Health"`, `"total":2`, `"note":"one row per day — the day's total"`, `"id":"other"`, `"id":"garden"`,
		`"tag":"Health"`, `"color":"#DC2626"`, `"tag":"Other"`, `"brand":{"mark":"♥","color":"#FF2D55","ink":"#FFFFFF","logo":"M12`, `"brand":{"mark":"G","color":"#64748B","ink":"#FFFFFF"}`} {
		if !strings.Contains(b, want) {
			t.Fatalf("missing %s in %s", want, b)
		}
	}
	for _, no := range []string{`"title":"Life app"`, `"id":"spend"`} {
		if strings.Contains(b, no) {
			t.Fatalf("unwanted %s shown in %s", no, b)
		}
	}
}

// Once a goal lists sources, Configuration groups them under that goal's
// title in blue, and what no goal reads goes last under "No goal".
func TestSourcesGroupedByGoal(t *testing.T) {
	s := newTest(t)
	for _, src := range []string{"health", "garden"} {
		if _, err := s.obs.Insert(obs.Observation{Source: src, Kind: "k", TS: time.Now(), Payload: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	made, err := s.goals.Create(goals.Goal{Title: "Make me healthier", Status: "active", Sources: "health, calendar"})
	if err != nil {
		t.Fatal(err)
	}
	w := s.do(t, "GET", "/api/v1/sources", nil)
	var v SourcesView
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Groups) != 2 {
		t.Fatalf("groups = %+v", v.Groups)
	}
	g, rest := v.Groups[0], v.Groups[1]
	if g.Tag != "Make me healthier" || g.Color != goalHex(made.Emblem.Hue) || len(g.Sources) != 1 || g.Sources[0].ID != "health" {
		t.Fatalf("goal group = %+v", g)
	}
	if rest.Tag != "No goal" || len(rest.Sources) != 1 || rest.Sources[0].ID != "garden" {
		t.Fatalf("rest = %+v", rest)
	}
}

// A rec filed by a session must carry the model that wrote it. The hub reads it off the session's run; a thread that has
// never run has none, so the body has to say it — and then it filters.
func TestRecsCarryTheirModel(t *testing.T) {
	s := newTest(t)
	if w := s.do(t, "POST", "/api/v1/recs", map[string]any{"title": "by a session", "source": "claude:thread:never-ran", "thread_id": "never-ran"}); w.Code != 400 {
		t.Fatal("model-less session rec accepted:", w.Code, w.Body.String())
	}
	if w := s.do(t, "POST", "/api/v1/recs", map[string]any{"title": "by a session", "source": "claude:thread:never-ran", "thread_id": "never-ran", "model": "claude-opus-5"}); w.Code != 201 || !strings.Contains(w.Body.String(), `"model":"claude-opus-5"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := s.do(t, "GET", "/api/v1/recs?model=claude-opus-5", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "by a session") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := s.do(t, "GET", "/api/v1/recs?model=claude-sonnet-5", nil); w.Code != 200 || strings.Contains(w.Body.String(), "by a session") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := s.do(t, "GET", "/api/v1/recs/stats", nil); !strings.Contains(w.Body.String(), `"by_model":[{"model":"claude-opus-5","total":1`) {
		t.Fatal(w.Body.String())
	}
}

// A rec card says when its session is working right now: the list and the
// single GET carry `thread_running` while the filing thread's status is
// `running`, and omit it otherwise — a rec the owner filed never has it.
func TestRecsSayWhenTheirSessionIsRunning(t *testing.T) {
	s, db := newTestDB(t)
	var th struct{ ID string }
	json.Unmarshal(s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": "file one"}).Body.Bytes(), &th)
	var bySession, byOwner struct{ ID string }
	json.Unmarshal(s.do(t, "POST", "/api/v1/recs", map[string]any{"title": "by a session", "thread_id": th.ID,
		"source": "claude:thread:" + th.ID, "model": "claude-opus-5"}).Body.Bytes(), &bySession)
	json.Unmarshal(s.do(t, "POST", "/api/v1/recs", map[string]any{"title": "by the owner", "source": "owner"}).Body.Bytes(), &byOwner)
	running := func(id string) bool {
		t.Helper()
		w := s.do(t, "GET", "/api/v1/recs/"+id, nil)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var r struct {
			ThreadRunning bool `json:"thread_running"`
		}
		json.Unmarshal(w.Body.Bytes(), &r)
		return r.ThreadRunning
	}
	listSays := func(id string) bool {
		t.Helper()
		var out struct {
			Recs []struct {
				ID            string
				ThreadRunning bool `json:"thread_running"`
			}
		}
		json.Unmarshal(s.do(t, "GET", "/api/v1/recs", nil).Body.Bytes(), &out)
		for _, r := range out.Recs {
			if r.ID == id {
				return r.ThreadRunning
			}
		}
		t.Fatal("rec not listed:", id)
		return false
	}
	if _, err := db.Exec(`UPDATE threads SET status='idle' WHERE id=?`, th.ID); err != nil {
		t.Fatal(err)
	}
	if running(bySession.ID) || listSays(bySession.ID) || running(byOwner.ID) {
		t.Fatal("marked running while nothing runs")
	}
	if w := s.do(t, "GET", "/api/v1/recs/"+bySession.ID, nil); strings.Contains(w.Body.String(), "thread_running") {
		t.Fatal("false should be omitted:", w.Body.String())
	}
	if _, err := db.Exec(`UPDATE threads SET status='running' WHERE id=?`, th.ID); err != nil {
		t.Fatal(err)
	}
	if !running(bySession.ID) || !listSays(bySession.ID) {
		t.Fatal("the filing session is running and the rec does not say so")
	}
	if running(byOwner.ID) || listSays(byOwner.ID) {
		t.Fatal("a rec the owner filed has no session to be running")
	}
}

// A session card says which model the session runs on: the
// list and the single GET carry `model` — the live run's, else the newest
// run that named one — and a thread that never ran with an explicit model
// carries none.
func TestThreadsCarryTheirModel(t *testing.T) {
	s, db := newTestDB(t)
	var a, b struct{ ID string }
	json.Unmarshal(s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": "one"}).Body.Bytes(), &a)
	json.Unmarshal(s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": "two"}).Body.Bytes(), &b)
	db.Exec(`DELETE FROM thread_runs`)
	db.Exec(`INSERT INTO thread_runs (id, thread_id, started_at, finished_at, trigger, model, out_file, in_file) VALUES ('r1',?,'2026-08-26T01:00:00Z','2026-08-26T01:10:00Z','message','claude-opus-5','','')`, a.ID)
	db.Exec(`INSERT INTO thread_runs (id, thread_id, started_at, trigger, model, out_file, in_file) VALUES ('r2',?,'2026-08-26T02:00:00Z','message','claude-fable-5','','')`, a.ID)
	db.Exec(`INSERT INTO thread_runs (id, thread_id, started_at, finished_at, trigger, model, out_file, in_file) VALUES ('r3',?,'2026-08-26T03:00:00Z','2026-08-26T03:10:00Z','message','','','')`, b.ID)
	var list []struct{ ID, Model string }
	json.Unmarshal(s.do(t, "GET", "/api/v1/threads", nil).Body.Bytes(), &list)
	got := map[string]string{}
	for _, x := range list {
		got[x.ID] = x.Model
	}
	if got[a.ID] != "claude-fable-5" || got[b.ID] != "" {
		t.Fatalf("list models %v", got)
	}
	if w := s.do(t, "GET", "/api/v1/threads/"+a.ID, nil); !strings.Contains(w.Body.String(), `"model":"claude-fable-5"`) {
		t.Fatal(w.Body.String())
	}
	if w := s.do(t, "GET", "/api/v1/threads/"+b.ID, nil); strings.Contains(w.Body.String(), `"model"`) {
		t.Fatal(w.Body.String())
	}
}

// The desktop app reports its build on launch (POST /api/v1/app/mac) — the
// Mac's device report, kept as a setting since the Mac is never a push
// device — and the "Install desktop build N" cards it satisfies close on the
// spot.
func TestMacBuildReportClosesDesktopInstallCards(t *testing.T) {
	s := newTest(t)
	th, _ := s.thr.Create("", "life", "", "Desktop install.", "", "", nil)
	a, _ := s.thr.AddAsk(th.ID, "", "Install desktop build 1512", "the install cell", "install", "")
	do := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/app/mac", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := do(`{}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do(`{"build":1510}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got, _ := s.thr.GetAsk(a.ID); got.State != "open" || got.Target != "mac" {
		t.Fatalf("closed by an older build: %+v", got)
	}
	if w := do(`{"build":1512}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got, _ := s.thr.GetAsk(a.ID); got.State != "done" || got.Resolution != "the desktop app reports build 1512" {
		t.Fatalf("%+v", got)
	}
	if s.thr.MacBuild() != 1512 {
		t.Fatalf("setting: %d", s.thr.MacBuild())
	}
}

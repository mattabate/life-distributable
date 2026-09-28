package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
	"time"

	"life/hub/internal/calendar"
	"life/hub/internal/obs"
	"life/hub/internal/store"
)

// The contract from real hub output (2026-09-26). Every GET the phone decodes
// is served by a seeded test hub and its reply written to
// shared/fixtures/<name>.json; the app's LifeTests decode those files with
// the app's own types and fail on a key the Swift struct does not declare. So
// a field added here reaches the phone's tests without anybody hand-writing
// JSON (the hand-written ContractDecodeTests drifted: Ask.class, a thread's
// speaking/waiting_to_speak, half of CalItem).
//
// The committed file is stale when the reply's SHAPE differs — its key paths,
// not its values (ids, timestamps and counts move every run). Regenerate with
//
//	UPDATE_FIXTURES=1 go test ./internal/server -run TestFixtures
func TestFixtures(t *testing.T) {
	s, db := newTestDB(t)
	ids := seedFixtures(t, s, db)

	dir := filepath.Join("..", "..", "..", "shared", "fixtures")
	update := os.Getenv("UPDATE_FIXTURES") == "1"
	if update {
		os.MkdirAll(dir, 0o755)
	}
	for _, f := range fixtureRoutes(ids) {
		w := s.do(t, "GET", f.path, nil)
		if w.Code != 200 {
			t.Errorf("%s: GET %s → %d %s", f.name, f.path, w.Code, w.Body)
			continue
		}
		fresh := normalizeFixture(t, w.Body.Bytes())
		file := filepath.Join(dir, f.name+".json")
		if update {
			if err := os.WriteFile(file, fresh, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		old, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("%s: no committed fixture (%v) — run with UPDATE_FIXTURES=1", f.name, err)
			continue
		}
		if added, gone := diffShape(shapeOf(t, old), shapeOf(t, fresh)); len(added)+len(gone) > 0 {
			t.Errorf("%s is stale (GET %s): new keys %v, keys gone %v — run with UPDATE_FIXTURES=1 and make the app's types match",
				file, f.path, added, gone)
		}
	}
	if !update {
		// A fixture nobody serves any more is a stale one too.
		want := map[string]bool{}
		for _, f := range fixtureRoutes(ids) {
			want[f.name+".json"] = true
		}
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if !want[e.Name()] {
				t.Errorf("shared/fixtures/%s is served by no route — delete it", e.Name())
			}
		}
	}
}

type fixtureRoute struct{ name, path string }

// fixtureRoutes: one row per GET in app/Life/Sources/HubClient.swift, with
// the query the app sends. The Swift side (FixtureDecodeTests) pairs each
// name with the type it decodes into.
func fixtureRoutes(id fixtureIDs) []fixtureRoute {
	return []fixtureRoute{
		{"spend_summary", "/api/v1/spend/summary?days=30"},
		{"spend_model", "/api/v1/spend/model"},
		{"usage", "/api/v1/usage"},
		{"projects", "/api/v1/projects"},
		{"sessions", "/api/v1/sessions"},
		{"actions", "/api/v1/actions?state=&limit=100"},
		{"actions_id", "/api/v1/actions/" + id.action},
		{"runs_id", "/api/v1/runs/1"},
		{"decider", "/api/v1/decider"},
		{"status", "/api/v1/status"},
		{"goals", "/api/v1/goals"},
		{"goals_id", "/api/v1/goals/" + id.goal},
		{"goals_id_notes", "/api/v1/goals/" + id.goal + "/notes?limit=30"},
		{"sources", "/api/v1/sources"},
		{"threads", "/api/v1/threads"},
		{"threads_id", "/api/v1/threads/" + id.thread},
		{"threads_id_messages", "/api/v1/threads/" + id.thread + "/messages?limit=200"},
		{"threads_id_events", "/api/v1/threads/" + id.thread + "/events?since=0&limit=1000"},
		{"threads_id_steps", "/api/v1/threads/" + id.thread + "/steps"},
		{"changes", "/api/v1/changes?since=&wait=0&thread="},
		{"calendar", "/api/v1/calendar?from=&to="},
		{"calendar_id", "/api/v1/calendar/" + id.cal},
		{"board", "/api/v1/board?surface=mobile"},
		{"asks", "/api/v1/asks?state=all&thread=" + id.thread},
		{"asks_id", "/api/v1/asks/" + id.ask},
		{"prompts", "/api/v1/prompts?state=queued&thread="},
		{"app_install_status", "/api/v1/app/install/status"},
		{"observations", "/api/v1/observations?source=health&kind=steps&limit=50"},
		{"recs", "/api/v1/recs?status=all&domain="},
		{"recs_id", "/api/v1/recs/" + id.rec},
		{"recs_id_starter", "/api/v1/recs/" + id.rec + "/starter"},
	}
}

type fixtureIDs struct{ goal, thread, ask, action, cal, rec string }

// seedFixtures fills the hub with one of everything, optional fields set,
// so each reply carries every key it can.
func seedFixtures(t *testing.T, s *Server, db *store.DB) fixtureIDs {
	t.Helper()
	var id fixtureIDs
	post := func(path string, body any, into any) {
		t.Helper()
		w := s.do(t, "POST", path, body)
		if w.Code != 200 && w.Code != 201 && w.Code != 202 {
			t.Fatalf("seed POST %s → %d %s", path, w.Code, w.Body)
		}
		if into != nil {
			if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
				t.Fatalf("seed POST %s: %v %s", path, err, w.Body)
			}
		}
	}
	now := time.Now()
	day := func(d int) string { return now.AddDate(0, 0, d).Format("2006-01-02") }
	var ref struct {
		ID string `json:"id"`
	}

	// A goal with a digest and a note of each author.
	post("/api/v1/goals", map[string]any{"title": "Get smarter", "statement": "Learn a thing a week",
		"horizon": "year", "cadence": "weekly", "sources": "learn"}, &ref)
	id.goal = ref.ID
	if w := s.do(t, "PATCH", "/api/v1/goals/"+id.goal, map[string]string{"digest": "Two tracks open."}); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	post("/api/v1/goals/"+id.goal+"/notes", map[string]any{"author": "owner", "kind": "review", "text": "On track"}, nil)

	// Two sessions: one running with a turn of events, one idle that is
	// speaking and has a line waiting.
	post("/api/v1/threads", map[string]any{"prompt": "Split my brokerage cash sensibly", "goal_id": id.goal}, &ref)
	id.thread = ref.ID
	db.Exec(`INSERT INTO thread_events (thread_id, run_id, ts, kind, title, body, summary) VALUES (?,'r1',?,'tool_use','Bash','ls','listed')`, id.thread, store.TS(now))
	db.Exec(`INSERT INTO thread_events (thread_id, run_id, ts, kind, title, body, summary) VALUES (?,'r1',?,'text','','Looking at the cash','')`, id.thread, store.TS(now))
	quiet, err := s.thr.Create("Water the plants", "life", id.goal, "p", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s.thr.Voice.Mark(quiet.ID, now.Add(time.Minute))
	release := s.thr.Voice.Wait(quiet.ID, "")
	t.Cleanup(release)

	// Asks: a decision on the running session, an install, a check.
	a, err := s.thr.AddAskOn(id.thread, "", "Move $2k to Broker B?", "Cash is idle", "decision", "", "any")
	if err != nil {
		t.Fatal(err)
	}
	id.ask = a.ID
	if _, err := s.thr.AddAskOn(quiet.ID, "", "Water the fern", "", "physical", "photo of the soil", "mobile"); err != nil {
		t.Fatal(err)
	}
	post("/api/v1/asks", map[string]any{"thread_id": id.thread, "title": "Read this", "detail": "a finding",
		"kind": "read", "surface": "any", "say": "One finding."}, nil)

	// A proposal, a prompt for later, a job run.
	var act struct {
		ID string `json:"id"`
	}
	post("/api/v1/actions", map[string]any{"kind": "money", "title": "move $5", "detail": "to savings", "exec_type": "none"}, &act)
	id.action = act.ID
	post("/api/v1/prompts", map[string]any{"target": id.thread, "text": "Check again", "in_reply_to": "ask:" + id.ask,
		"outcome": "done", "on": day(3), "at_time": "09:00"}, nil)
	raw := `{"is_error":false,"result":"Sweep done.\n\n{\"summary\":\"all quiet\",\"findings\":[{\"kind\":\"commit\",\"title\":\"commit x\",\"detail\":\"why\"}],\"needs_you\":[\"renew passport\"]}","total_cost_usd":0.5}`
	db.Exec(`INSERT INTO runs (id, job, started_at, finished_at, ok, summary, output, cost_usd) VALUES (1,'sweep',?,?,1,'all quiet',?,0.5)`,
		store.TS(now.Add(-time.Hour)), store.TS(now.Add(-50*time.Minute)), raw)

	// Calendar: overdue, due today, a soon to-do, an agent step, a repeat.
	must := func(it calendar.Item) calendar.Item {
		t.Helper()
		out, err := s.Cal.Add(it)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	id.cal = must(calendar.Item{Title: "Water the plants", Detail: "all of them", Day: day(0), At: "09:00", Kind: "owner",
		Due: "on", GoalID: id.goal, ThreadID: quiet.ID, Repeat: "weekly", NagMin: 30, Surface: "mobile", Say: "Water the plants.",
		AskKind: "physical", CheckHint: "a photo"}).ID
	must(calendar.Item{Title: "Renew parking", Day: day(-2), Kind: "owner", Due: "by", ThreadID: id.thread})
	must(calendar.Item{Title: "Buy a fern", Kind: "owner", Soon: true})
	must(calendar.Item{Title: "Re-check cash", Day: day(1), At: "10:00", Kind: "agent", ThreadID: id.thread})
	done := must(calendar.Item{Title: "Call the vet", Day: day(-1), Kind: "owner", ThreadID: id.thread})
	s.Cal.Resolve(done.ID, "done", "owner", "Booked for Tuesday")

	// Recs: one open with every field, one decided.
	post("/api/v1/recs", map[string]any{"title": "Try Semantic Scholar's API key", "detail": "free tier",
		"domain": "audience", "kind": "try", "cost_cents": 1100, "cost_period": "monthly", "effort": "low",
		"confidence": 70, "because": "rate limits", "expect": "fewer 429s", "act_by": day(7), "review_on": day(30),
		"goal_id": id.goal, "thread_id": id.thread, "source": "claude:thread:" + id.thread, "model": "claude-opus-5"}, &ref)
	id.rec = ref.ID
	var r2 struct {
		ID string `json:"id"`
	}
	post("/api/v1/recs", map[string]any{"title": "Cancel the gym", "domain": "money", "kind": "stop", "cost_cents": 4000,
		"cost_period": "monthly", "source": "owner"}, &r2)
	post("/api/v1/recs/"+r2.ID+"/decide", map[string]any{"status": "accepted", "note": "yes", "by": "owner"}, nil)

	// A finished app install's log (synthetic: team, host and token are placeholders).
	logPath := s.cfg.OpsPath("logs", "app-install.log")
	os.MkdirAll(filepath.Dir(logPath), 0o755)
	os.WriteFile(logPath, []byte("▶ archiving 1.0.100 (team TEAMID0000)…\n▶ exporting ad-hoc .ipa…\n"+
		"✔ published 1.0.100 (100) ad-hoc, profile → 2027-01-01, aps=production\n"+
		"  open on the phone: https://my-mac.example.ts.net:8443/ota/0000/install.html\nexit=0\n"), 0o644)

	// Observations: a few days of step counts from the phone.
	for i := 0; i < 3; i++ {
		pl, _ := json.Marshal(map[string]any{"key": fmt.Sprintf("s%d", i), "count": 8000 + i*500, "day": day(-i)})
		if _, err := s.obs.Insert(obs.Observation{Source: "health", Kind: "steps", TS: now.AddDate(0, 0, -i), Payload: pl}); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// normalizeFixture: the reply pretty-printed, every minted id's random half
// replaced by a counter in order of first sight — so a fixture reads the
// same from run to run and its ids still look like ids.
var mintedHex = regexp.MustCompile(`(-)([0-9a-f]{8})\b`)

func normalizeFixture(t *testing.T, body []byte) []byte {
	t.Helper()
	seen := map[string]string{}
	out := mintedHex.ReplaceAllStringFunc(string(body), func(m string) string {
		h := m[1:]
		if _, ok := seen[h]; !ok {
			seen[h] = fmt.Sprintf("%08x", len(seen)+1)
		}
		return "-" + seen[h]
	})
	var buf bytes.Buffer
	if err := json.Indent(&buf, bytes.TrimSpace([]byte(out)), "", "  "); err != nil {
		t.Fatalf("reply is not JSON: %v", err)
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}

// shapeOf: the set of key paths in a JSON document — `.threads[].speaking`.
// Keys that are data (ids, days, numbers) collapse to `*`, so a map keyed by
// thread id or by date has one shape however many entries it holds.
var dataKey = regexp.MustCompile(`^(\d.*|.*-[0-9a-f]{8}|[A-Z0-9_]+|.*[:/ ].*)$`)

func shapeOf(t *testing.T, doc []byte) map[string]bool {
	t.Helper()
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	var walk func(p string, v any)
	walk = func(p string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				if dataKey.MatchString(k) {
					k = "*"
				}
				out[p+"."+k] = true
				walk(p+"."+k, c)
			}
		case []any:
			for _, c := range x {
				walk(p+"[]", c)
			}
		}
	}
	walk("", v)
	return out
}

func diffShape(old, fresh map[string]bool) (added, gone []string) {
	for k := range fresh {
		if !old[k] {
			added = append(added, k)
		}
	}
	for k := range old {
		if !fresh[k] {
			gone = append(gone, k)
		}
	}
	sort.Strings(added)
	sort.Strings(gone)
	return added, gone
}

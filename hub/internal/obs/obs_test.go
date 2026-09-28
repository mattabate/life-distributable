package obs

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"life/hub/internal/store"
)

func TestObservationsAndBlobs(t *testing.T) {
	dir := t.TempDir()
	db, _ := store.Open(filepath.Join(dir, "t.db"))
	s, err := New(db, filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	ref, n, err := s.PutBlob(strings.NewReader("hello"), "JPG")
	if err != nil || n != 5 || !strings.HasPrefix(ref, "sha256/2c/2cf24dba") || !strings.HasSuffix(ref, ".jpg") {
		t.Fatal(ref, n, err)
	}
	ref2, _, _ := s.PutBlob(strings.NewReader("hello"), "jpg")
	if ref2 != ref {
		t.Fatal("not content-addressed")
	}
	if _, err := s.BlobPath("../../etc/passwd"); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err := s.BlobPath(ref); err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	o, err := s.Insert(Observation{Source: "app", Kind: "photo", TS: ts, Payload: json.RawMessage(`{"note":"lunch"}`), BlobRef: ref})
	if err != nil || o.ID == 0 || o.SchemaVersion != 1 {
		t.Fatal(o, err)
	}
	if _, err := s.Insert(Observation{Source: "app", Kind: "note", Payload: json.RawMessage(`nope`)}); err == nil {
		t.Fatal("bad payload accepted")
	}
	s.Insert(Observation{Source: "app", Kind: "note", TS: ts.Add(time.Hour), Payload: json.RawMessage(`{"text":"x"}`)})
	all, _ := s.List(Query{})
	photos, _ := s.List(Query{Kind: "photo"})
	late, _ := s.List(Query{Since: ts.Add(30 * time.Minute)})
	if len(all) != 2 || len(photos) != 1 || len(late) != 1 || all[0].Kind != "note" {
		t.Fatal(len(all), len(photos), len(late))
	}
	cs, _ := s.Counts()
	if len(cs) != 2 {
		t.Fatal(cs)
	}
}

// "Give me everything" must not come back as a page. A caller asking for more
// rows than the ceiling used to get the DEFAULT 100 instead, silently: the
// website section asked for 20,000 daily rows, got 100, and printed 22
// visitors over "6 days measured" against a 133-day history (2026-08-29).
func TestListBigLimitIsNotSilentlyOneHundred(t *testing.T) {
	dir := t.TempDir()
	db, _ := store.Open(filepath.Join(dir, "t.db"))
	s, _ := New(db, filepath.Join(dir, "blobs"))
	var rows []Observation
	for i := 0; i < 150; i++ {
		rows = append(rows, Observation{Source: "posthog", Kind: "site-day",
			TS:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i),
			Payload: json.RawMessage(`{"visitors":1}`), UniqKey: fmt.Sprintf("d%d", i)})
	}
	if _, _, err := s.InsertBatch(rows); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.List(Query{Source: "posthog", Limit: 20000}); len(got) != 150 {
		t.Fatalf("asked for 20000 of 150 rows, got %d", len(got))
	}
	// Unset still means one page, so nothing that never passed a limit changes.
	if got, _ := s.List(Query{Source: "posthog"}); len(got) != 100 {
		t.Fatalf("default page = %d rows, want 100", len(got))
	}
}

// One account's rows are filtered in SQL, so a row limit counts only them.
func TestListByAccount(t *testing.T) {
	dir := t.TempDir()
	db, _ := store.Open(filepath.Join(dir, "t.db"))
	s, _ := New(db, filepath.Join(dir, "blobs"))
	for i, a := range []string{"a", "b", "a", "b", "b"} {
		s.Insert(Observation{Source: "simplefin", Kind: "transaction", TS: time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC),
			Payload: json.RawMessage(`{"account":"` + a + `","amount":1}`)})
	}
	if got, _ := s.List(Query{Source: "simplefin", Kind: "transaction", Account: "a", Limit: 5}); len(got) != 2 {
		t.Fatalf("account a: %d rows, want 2", len(got))
	}
}

// A connector that re-reads a window it already sent (late-arriving HealthKit
// samples) must not duplicate it: uniq_key is the row's identity inside
// (source, kind). Rows without a key are unaffected.
// 2026-09-13: every Health day since 08-24 held the first partial sync after
// midnight (09-12 = 27 steps). A later, different reading of the same day must
// become what readers see — as a new row, the old one untouched.
func TestLaterHealthReadingSupersedesTheFirst(t *testing.T) {
	dir := t.TempDir()
	db, _ := store.Open(filepath.Join(dir, "t.db"))
	s, _ := New(db, filepath.Join(dir, "blobs"))
	steps := func(payload string) Observation {
		return Observation{Source: "health", Kind: "steps", TS: time.Date(2026, 9, 12, 4, 0, 0, 0, time.UTC),
			Payload: json.RawMessage(payload), UniqKey: "steps:2026-09-12"}
	}
	first, err := s.Insert(steps(`{"value":27,"unit":"count","date":"2026-09-12"}`))
	if err != nil {
		t.Fatal(err)
	}
	// same reading, keys in another order: still a duplicate
	if ins, skip, _ := s.InsertBatch([]Observation{steps(`{"date":"2026-09-12","value":27,"unit":"count"}`)}); ins != 0 || skip != 1 {
		t.Fatalf("reordered payload: ins %d skip %d", ins, skip)
	}
	if ins, _, _ := s.InsertBatch([]Observation{steps(`{"value":9120,"unit":"count","date":"2026-09-12"}`)}); ins != 1 {
		t.Fatal("later reading skipped")
	}
	// and a third: the chain keeps going, and a repeat of the newest is a dupe
	third, err := s.Insert(steps(`{"value":9544,"unit":"count","date":"2026-09-12"}`))
	if err != nil || third.UniqKey != "steps:2026-09-12~r2" {
		t.Fatal(third, err)
	}
	if _, err := s.Insert(steps(`{"value":9544,"unit":"count","date":"2026-09-12"}`)); err != ErrDuplicate {
		t.Fatal("repeat of newest reading:", err)
	}
	rows, _ := s.List(Query{Source: "health", Kind: "steps"})
	if len(rows) != 1 || !strings.Contains(string(rows[0].Payload), "9544") || BaseKey(rows[0].UniqKey) != "steps:2026-09-12" {
		t.Fatalf("readers see %+v", rows)
	}
	if c, _ := s.Counts(); len(c) != 1 || c[0].N != 1 {
		t.Fatalf("counts %+v", c)
	}
	// append-only: the first partial reading is still stored, unchanged
	var p string
	db.QueryRow(`SELECT payload FROM observations WHERE id=?`, first.ID).Scan(&p)
	if !strings.Contains(p, `"value":27`) {
		t.Fatal("first row changed:", p)
	}
	// a non-revisable source keeps first-write-wins
	app := Observation{Source: "device", Kind: "cycle", Payload: json.RawMessage(`{"n":1}`), UniqKey: "c1"}
	s.Insert(app)
	app.Payload = json.RawMessage(`{"n":2}`)
	if _, err := s.Insert(app); err != ErrDuplicate {
		t.Fatal("device revised:", err)
	}
}

// LatestBy returns one row per payload identity — the newest — however many
// copies are stored, and a revisable source/kind entry revises only that kind.
func TestLatestByAndPerKindRevisable(t *testing.T) {
	dir := t.TempDir()
	db, _ := store.Open(filepath.Join(dir, "t.db"))
	s, _ := New(db, filepath.Join(dir, "blobs"))
	for day := 1; day <= 3; day++ {
		for copy := 0; copy < 5; copy++ {
			pl := fmt.Sprintf(`{"project_id":7,"date":"2026-09-%02d","views":%d}`, day, copy)
			s.Insert(Observation{Source: "posthog", Kind: "site-day", TS: time.Date(2026, 9, day, 16, 0, 0, 0, time.UTC), Payload: json.RawMessage(pl)})
		}
	}
	rows, err := s.List(Query{Source: "posthog", Kind: "site-day", LatestBy: []string{"project_id", "date"}, Limit: 2})
	if err != nil || len(rows) != 2 || !strings.Contains(string(rows[0].Payload), `"2026-09-03","views":4`) {
		t.Fatalf("latest = %v %v", rows, err)
	}
	if all, _ := s.List(Query{Source: "posthog", Kind: "site-day", LatestBy: []string{"project_id", "date"}, Limit: 100}); len(all) != 3 {
		t.Fatalf("want one row per day, got %d", len(all))
	}
	if _, err := s.List(Query{LatestBy: []string{"x') OR 1=1 --"}}); err == nil {
		t.Fatal("unsafe field name accepted")
	}
	// posthog/site-day is revisable, posthog/site-mix is not
	key := func(kind, pl string) error {
		_, err := s.Insert(Observation{Source: "posthog", Kind: kind, Payload: json.RawMessage(pl), UniqKey: "k"})
		return err
	}
	key("site-day", `{"n":1}`)
	if err := key("site-day", `{"n":2}`); err != nil {
		t.Fatal("site-day not revised:", err)
	}
	key("site-mix", `{"n":1}`)
	if err := key("site-mix", `{"n":2}`); err != ErrDuplicate {
		t.Fatal("site-mix revised:", err)
	}
}

// ts and ingested_at are written in store.TS's fixed form, and a Since bound
// on a whole second still includes both a new row and a legacy trimmed one
// ("…T04:00:00Z", RFC3339Nano before 2026-09-14) at that instant.
func TestTimestampsAreFixedWidthAndBoundsSeeLegacyRows(t *testing.T) {
	dir := t.TempDir()
	db, _ := store.Open(filepath.Join(dir, "t.db"))
	s, _ := New(db, filepath.Join(dir, "blobs"))
	at := time.Date(2026, 9, 12, 4, 0, 0, 0, time.UTC)
	if _, err := s.Insert(Observation{Source: "health", Kind: "steps", TS: at, Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	var ts, ing string
	db.QueryRow(`SELECT ts, ingested_at FROM observations`).Scan(&ts, &ing)
	if ts != "2026-09-12T04:00:00.000000000Z" || len(ing) != len(ts) {
		t.Fatalf("ts %q ingested_at %q", ts, ing)
	}
	db.Exec(`INSERT INTO observations (source,kind,ts,payload) VALUES ('health','steps','2026-09-12T04:00:00Z','{}')`)
	if rows, _ := s.List(Query{Source: "health", Since: at, Until: at.Add(time.Second)}); len(rows) != 2 {
		t.Fatalf("rows at the bound = %d, want 2", len(rows))
	}
}

func TestBatchDedupesOnUniqKey(t *testing.T) {
	dir := t.TempDir()
	db, _ := store.Open(filepath.Join(dir, "t.db"))
	s, _ := New(db, filepath.Join(dir, "blobs"))
	day := func(d int) Observation {
		return Observation{Source: "health", Kind: "steps", TS: time.Date(2026, 8, d, 0, 0, 0, 0, time.UTC),
			Payload: json.RawMessage(`{"value":8000}`), UniqKey: fmt.Sprintf("steps:2026-08-%02d", d)}
	}
	ins, skip, err := s.InsertBatch([]Observation{day(18), day(19), day(20)})
	if err != nil || ins != 3 || skip != 0 {
		t.Fatalf("first sync: %d %d %v", ins, skip, err)
	}
	// the next sync re-reads the last two days and adds one
	ins, skip, err = s.InsertBatch([]Observation{day(19), day(20), day(21)})
	if err != nil || ins != 1 || skip != 2 {
		t.Fatalf("re-sync: %d %d %v", ins, skip, err)
	}
	if all, _ := s.List(Query{Source: "health"}); len(all) != 4 {
		t.Fatalf("rows: %d", len(all))
	}
	// the same key under a different kind is a different row
	if _, err := s.Insert(Observation{Source: "health", Kind: "sleep", UniqKey: "steps:2026-08-20", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	// keyless rows still always insert
	for i := 0; i < 2; i++ {
		if _, err := s.Insert(Observation{Source: "app", Kind: "note", Payload: json.RawMessage(`{"text":"x"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if notes, _ := s.List(Query{Source: "app"}); len(notes) != 2 {
		t.Fatalf("keyless: %d", len(notes))
	}
	// a bad row fails the whole batch rather than half-writing it
	if _, _, err := s.InsertBatch([]Observation{day(22), {Source: "health", Kind: "", UniqKey: "x"}}); err == nil {
		t.Fatal("invalid item accepted")
	}
	if all, _ := s.List(Query{Source: "health", Kind: "steps"}); len(all) != 4 {
		t.Fatalf("a failed batch half-wrote: %d rows", len(all))
	}
}

func newStore(t *testing.T) (*Store, *store.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(db, filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	return s, db
}

// One Ingest: tz is always an IANA name, a blob_ref must name a stored blob,
// a writer's Supersedes is kept (and hides the old row), and a registered
// hook hears each call's NEW rows once, whichever wrapper wrote them.
func TestIngestRules(t *testing.T) {
	s, _ := newStore(t)
	for in, want := range map[string]string{"": Zone, "EDT": Zone, "EST": Zone, "UTC": Zone, "Local": Zone,
		"America/New_York": Zone, "Europe/Paris": "Europe/Paris", "Nowhere/Land": Zone} {
		if got := CanonicalTZ(in); got != want {
			t.Errorf("CanonicalTZ(%q) = %q, want %q", in, got, want)
		}
	}
	var heard [][]Observation
	s.Register(Kind{Kind: "workout", OnInsert: func(rows []Observation) { heard = append(heard, rows) }})
	w := func(k string) Observation {
		return Observation{Source: "health", Kind: "workout", UniqKey: k, Payload: json.RawMessage(`{}`),
			TS: time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("EDT", -4*3600))}
	}
	r, err := s.Ingest([]Observation{w("a"), w("b"), {Source: "app", Kind: "note"}})
	if err != nil || r.Inserted != 3 || r.Rows[0].TZ != Zone {
		t.Fatalf("ingest %+v %v", r, err)
	}
	s.InsertBatch([]Observation{w("a"), w("c")}) // a is a duplicate
	s.Insert(w("c"))                             // all duplicate: no call
	if len(heard) != 2 || len(heard[0]) != 2 || len(heard[1]) != 1 || heard[1][0].UniqKey != "c" {
		t.Fatalf("hook heard %v", heard)
	}
	if _, err := s.Insert(Observation{Source: "app", Kind: "photo", BlobRef: "sha256/ab/abcd.jpg"}); err == nil {
		t.Fatal("a ref with no blob behind it was stored")
	}
	old, _ := s.Insert(Observation{Source: "mail", Kind: "purchase", UniqKey: "m1", Payload: json.RawMessage(`{"total":0}`)})
	s.Insert(Observation{Source: "mail", Kind: "purchase", UniqKey: "m1|v2", Supersedes: old.ID, Payload: json.RawMessage(`{"total":9}`)})
	if rows, _ := s.List(Query{Source: "mail"}); len(rows) != 1 || rows[0].Supersedes != old.ID {
		t.Fatalf("mail rows %+v", rows)
	}
}

// after_id pages forward by id, oldest first, whatever the rows' ts.
func TestListAfterID(t *testing.T) {
	s, _ := newStore(t)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var ids []int64
	for i, off := range []int{3, 1, 2, 0} { // written out of ts order
		o, _ := s.Insert(Observation{Source: "console", Kind: "trail", TS: base.Add(time.Duration(off) * time.Hour),
			Payload: json.RawMessage(fmt.Sprintf(`{"i":%d}`, i))})
		ids = append(ids, o.ID)
	}
	page, _ := s.List(Query{Source: "console", AfterID: ids[0], Limit: 2})
	if len(page) != 2 || page[0].ID != ids[1] || page[1].ID != ids[2] {
		t.Fatalf("page %+v", page)
	}
	rest, _ := s.List(Query{Source: "console", AfterID: page[1].ID, Limit: 2})
	if len(rest) != 1 || rest[0].ID != ids[3] {
		t.Fatalf("rest %+v", rest)
	}
}

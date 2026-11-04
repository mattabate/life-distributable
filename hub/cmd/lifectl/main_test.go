package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A job runs as the owner with `lifectl ask:*`: an @file or stdin must never
// carry a secret or the database onto a card.
func TestMayReadRefusesSecretsAndData(t *testing.T) {
	old := lifeRoot
	defer func() { lifeRoot = old }()
	lifeRoot = t.TempDir()
	for _, d := range []string{"ops/secrets", "data/tmp", "data/people", "docs"} {
		os.MkdirAll(filepath.Join(lifeRoot, d), 0o755)
	}
	put := func(rel string) string {
		p := filepath.Join(lifeRoot, rel)
		os.WriteFile(p, []byte("x"), 0o600)
		return p
	}
	tok, db := put("ops/secrets/hub.token"), put("data/life.db")
	link := filepath.Join(lifeRoot, "data/tmp/look.md")
	os.Symlink(tok, link)
	for p, ok := range map[string]bool{
		tok: false, db: false, link: false,
		filepath.Join(lifeRoot, "data/tmp/../../ops/secrets/hub.token"): false,
		put("data/other.json"):   false,
		put("data/tmp/draft.md"): true,
		put("data/people/a.md"):  true,
		put("docs/x.md"):         true,
	} {
		if err := mayRead(p); (err == nil) != ok {
			t.Errorf("%s: %v, want ok=%v", p, err, ok)
		}
	}
	f, _ := os.Open(tok)
	defer f.Close()
	stdin := os.Stdin
	defer func() { os.Stdin = stdin }()
	os.Stdin = f
	if mayRead("") == nil {
		t.Error("stdin from the token accepted")
	}
}

// Every top-level command in main's switch appears in the usage text, so
// `lifectl` with no arguments lists the whole tool (review 2026-09-26: the
// one-line usage had lost push, budget, dismiss, feed, cl and more).
func TestUsageListsEveryCommand(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	cases := regexp.MustCompile(`(?m)^\tcase ("[^:]+"):$`).FindAllStringSubmatch(string(src), -1)
	if len(cases) < 20 {
		t.Fatalf("found only %d top-level cases — did the switch move?", len(cases))
	}
	word := func(w string) bool {
		return regexp.MustCompile(`(^|[\s|·(\[])` + regexp.QuoteMeta(w) + `($|[\s|\]])`).MatchString(usageText)
	}
	for _, c := range cases {
		for _, name := range strings.Split(c[1], ",") {
			name = strings.Trim(strings.TrimSpace(name), `"`)
			if name == "calendar" { // the long alias of cal
				continue
			}
			if !word(name) {
				t.Errorf("usage text does not list %q", name)
			}
		}
	}
}

func TestActor(t *testing.T) {
	t.Setenv("LIFE_THREAD_ID", "")
	if got := actor(); got != "owner" {
		t.Errorf("no session: %q", got)
	}
	t.Setenv("LIFE_THREAD_ID", "th-1")
	if got := actor(); got != "claude:thread:th-1" {
		t.Errorf("session: %q", got)
	}
}

// Lists print one line per row: a 400 KB recs dump was clipped by the
// agent's tool anyway.
func TestPrintRows(t *testing.T) {
	var v any
	json.Unmarshal([]byte(`{"open":[{"id":"rec-1","state":"open","goal_id":"g","title":"Try X\nlong detail","detail":"skip me"}],"done":[],"thread":{"id":"th-1","status":"idle","title":"T"}}`), &v)
	var b strings.Builder
	if n := printRows(&b, "", v); n != 2 {
		t.Fatalf("%d rows:\n%s", n, b.String())
	}
	want := "open:\nrec-1  open  goal g  Try X\nth-1  idle  T\n"
	if b.String() != want {
		t.Fatalf("got\n%q\nwant\n%q", b.String(), want)
	}
}

// A calendar day is {day, entries}: its rows print under it, not an empty day.
func TestPrintRowsCalendarDays(t *testing.T) {
	var v any
	json.Unmarshal([]byte(`{"days":[{"day":"2030-05-29","entries":[{"id":"cal-1","kind":"note","day":"2030-05-29","title":"Dentist"}]},{"day":"2030-05-30","entries":[]}]}`), &v)
	var b strings.Builder
	if n := printRows(&b, "", v); n != 1 {
		t.Fatalf("%d rows:\n%s", n, b.String())
	}
	want := "days:\n2030-05-29\n  cal-1  note  2030-05-29  Dentist\n2030-05-30\n"
	if b.String() != want {
		t.Fatalf("got\n%q\nwant\n%q", b.String(), want)
	}
}

// An away span's line says which days it covers.
func TestPrintRowsAwaySpan(t *testing.T) {
	var v any
	json.Unmarshal([]byte(`{"away":[{"id":"away-1","state":"on","from":"2030-05-31","until":"2030-06-12","title":"Trip"}]}`), &v)
	var b strings.Builder
	printRows(&b, "", v)
	if want := "away:\naway-1  on  2030-05-31  2030-06-12  Trip\n"; b.String() != want {
		t.Fatalf("got\n%q\nwant\n%q", b.String(), want)
	}
}

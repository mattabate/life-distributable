package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

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

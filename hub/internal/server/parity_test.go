package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Surface parity, made mechanical: everything on the console exists on the
// phone, and vice versa.
//
// The failure this test exists to prevent: a page is built on the console
// and later found missing on the phone. So every page the console registers must name
// the phone screen that shows the same thing, and every top-level phone screen
// must name its console page. What is NOT required is that they look alike:
// the console draws a wide table, the phone draws sections and a search
// field, off the same route with the same numbers and the same words.
//
// A page that genuinely belongs to one surface only is allowed, WITH its
// reason — an empty phone file in places.js, or phoneOnly below. The
// exception is fine; staying silent about it is what turns into a missing
// page six weeks later.
//
// The pairs are NOT written here: they are read out of web/places.js, the
// table the console itself uses to tell a new session where the owner was standing
// (route, URL, source files) when they hit "+ New session".
// One table, so a page cannot name FinanceView.swift to the agent and
// SpendView.swift to this test. A row with an empty phone file is
// console-only and carries its why there.
type surfacePair struct {
	page   string // views.<page>, in hub/internal/server/web/views/*.js
	screen string // the SwiftUI screen showing the same thing
	file   string // app/Life/Sources/<file>, which must declare it
	view   string // web/<view>, the file that registers views.<page>
}

const placesFile = "web/places.js"

// placeRow: `  spend: { label: 'Spend', view: 'views/spend.js', phone: 'SpendView.swift'`
var placeRow = regexp.MustCompile(`(?m)^  ([a-z]+): \{ label: '([^']*)', view: '([^']*)', phone: '([^']*)'`)

// deepRow: `    { at: 3, label: 'one session', phone: 'ThreadDetailView.swift' },`
var deepRow = regexp.MustCompile(`(?m)^    \{ at: \d+, label: '[^']*', phone: '([^']*)' \},`)

// alsoRow: `    also: ['views/calgrid.js', 'CalendarGrid.swift'] },` — every
// file a page names beside its own two, whichever side it is on.
var alsoRow = regexp.MustCompile(`also: \[([^\]]*)\]`)

func alsoFiles(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(placesFile)
	if err != nil {
		t.Fatalf("read %s: %v", placesFile, err)
	}
	var out []string
	for _, m := range alsoRow.FindAllStringSubmatch(string(b), -1) {
		for _, f := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(m[1], -1) {
			out = append(out, f[1])
		}
	}
	return out
}

func places(t *testing.T) (pairs []surfacePair, only map[string]string, deep []string) {
	t.Helper()
	b, err := os.ReadFile(placesFile)
	if err != nil {
		t.Fatalf("read %s: %v", placesFile, err)
	}
	src := string(b)
	rows := placeRow.FindAllStringSubmatch(src, -1)
	if len(rows) < 6 { // v0: Sessions, Recs, Calendar, Spend, Goals, Sources (+ console-only rows)
		t.Fatalf("only %d rows parsed out of %s — the table's shape has drifted from placeRow", len(rows), placesFile)
	}
	only = map[string]string{}
	for _, m := range rows {
		page, label, view, phone := m[1], m[2], m[3], m[4]
		if label == "" {
			t.Errorf("places.js: %q has no label", page)
		}
		if phone == "" {
			why := regexp.MustCompile(`(?s)` + page + `: \{ label:.*?why: '([^']*)'`).FindStringSubmatch(src)
			if why == nil {
				t.Errorf("places.js: %q has no phone screen and no `why` — a console-only page says so out loud (docs/design/conventions.md rule 11)", page)
				continue
			}
			only[page] = why[1]
			continue
		}
		pairs = append(pairs, surfacePair{page: page, screen: strings.TrimSuffix(phone, ".swift"), file: phone, view: view})
	}
	for _, m := range deepRow.FindAllStringSubmatch(src, -1) {
		deep = append(deep, m[1])
	}
	return pairs, only, deep
}

// Phone screens with no console page, and why not.
var phoneOnly = map[string]string{
	"MoreView": "the More tab is a menu, not a page — the console's nav bar is the same list",
	"SettingsView": "phone only: the console has no Settings page — settings are changed by the agent " +
		"or by commands in the terminal, not by input boxes. What is left on the phone cannot exist on the console: the hub " +
		"address, the hub token and the decider code live in THIS DEVICE's Keychain, unreachable from the Mac, " +
		"and without them the app cannot reach the hub at all. Parity is about information; this is a credential " +
		"the console does not have and must never hold.",
}

const (
	viewsDir = "web/views"
	appDir   = "../../../app/Life/Sources"
)

func consolePages(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(viewsDir, "*.js"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no console views found in %s: %v", viewsDir, err)
	}
	re := regexp.MustCompile(`(?m)^views\.([a-zA-Z]+)\s*=`)
	pages := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			pages[m[1]] = filepath.Base(f)
		}
	}
	return pages
}

// phoneScreens: what the phone's tab bar and its More list actually open —
// the top-level pages, not every subview pushed from one.
func phoneScreens(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(appDir, "RootView.swift"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	var out []string
	tab := regexp.MustCompile(`Tab\("[^"]+"[^{]*\{\s*(?:NavigationStack\s*\{\s*)?([A-Za-z]+)`)
	dest := regexp.MustCompile(`(?m)^\s*case \.[a-zA-Z]+:\s*([A-Za-z]+)\(`)
	for _, re := range []*regexp.Regexp{tab, dest} {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			out = append(out, m[1])
		}
	}
	if len(out) < 6 {
		t.Fatalf("only found %d phone screens in RootView.swift (%v) — the regexes have drifted from the file", len(out), out)
	}
	return out
}

func TestEverySurfacePageExistsOnBothSurfaces(t *testing.T) {
	surfacePairs, surfaceOnly, deepScreens := places(t)
	paired := map[string]surfacePair{}
	for _, p := range surfacePairs {
		if _, dup := paired[p.page]; dup {
			t.Fatalf("%s is paired twice", p.page)
		}
		paired[p.page] = p
	}

	// 1. Every console page is paired, or excused out loud.
	for page, file := range consolePages(t) {
		if _, ok := paired[page]; ok {
			continue
		}
		if why := surfaceOnly[page]; why != "" {
			continue
		}
		t.Errorf("console page %q (%s) has no phone screen.\n"+
			"Information on the web must be on the phone too. Either build the\n"+
			"screen and add its row to web/places.js, or give that row `phone: ''` and a `why`\n"+
			"saying it is console-only (docs/design/conventions.md rule 11).", page, file)
	}

	// 2. The screen each pair names really is there — and so does the console
	// file the same row promises a session it will find the page in.
	for _, p := range surfacePairs {
		file, ok := consolePages(t)[p.page]
		if !ok {
			t.Errorf("places.js names console page %q, which no views/*.js registers", p.page)
		} else if want := "views/" + file; want != p.view {
			t.Errorf("places.js sends a session to %s for %q, but views.%s is registered in %s", p.view, p.page, p.page, want)
		}
		b, err := os.ReadFile(filepath.Join(appDir, p.file))
		if err != nil {
			t.Errorf("phone screen for %q: %v", p.page, err)
			continue
		}
		if !strings.Contains(string(b), "struct "+p.screen) {
			t.Errorf("app/Life/Sources/%s does not declare %s (the phone half of %q)", p.file, p.screen, p.page)
		}
	}

	// 2b. A sub-route names a file too ("one account" → AccountHistoryView).
	// Nothing pairs those — they are pushed, not tabs — but a session told to
	// open a file that does not exist is worse than one told nothing.
	for _, f := range deepScreens {
		if _, err := os.Stat(filepath.Join(appDir, f)); err != nil {
			t.Errorf("places.js deep route names app/Life/Sources/%s, which does not exist: %v", f, err)
		}
	}

	// 2c. `also` names a SECOND file on either side of one page (the calendar
	// is a page plus a grid). Same reason as 2b: a snap that points a session
	// at a file that is not there is worse than one that points nowhere.
	for _, f := range alsoFiles(t) {
		dir := "web"
		if strings.HasSuffix(f, ".swift") {
			dir = appDir
		}
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("places.js `also` names %s/%s, which does not exist: %v", dir, f, err)
		}
	}

	// 3. And the other direction: nothing the phone opens is missing from the
	// console ("and vice versa").
	onPhone := map[string]bool{}
	for _, p := range surfacePairs {
		onPhone[p.screen] = true
	}
	for _, s := range phoneScreens(t) {
		if onPhone[s] || phoneOnly[s] != "" {
			continue
		}
		t.Errorf("phone screen %s has no console page.\n"+
			"Build the views/<page>.js half and add it to surfacePairs, or add %q to phoneOnly\n"+
			"with the reason (docs/design/conventions.md rule 11).", s, s)
	}

	// 4. An exception without a reason is not an exception.
	for page, why := range surfaceOnly {
		if strings.TrimSpace(why) == "" {
			t.Errorf("surfaceOnly[%q] has no reason", page)
		}
	}
	for screen, why := range phoneOnly {
		if strings.TrimSpace(why) == "" {
			t.Errorf("phoneOnly[%q] has no reason", screen)
		}
	}
}

// Ask (the camera button that starts a session from the screen you are on) is a
// toolbar item each screen adds for itself (`.askButton()`, Snap.swift), so a
// new pushed page has none until someone remembers — one page shipped
// without it. Every `.navigationTitle(` is a page: it must be followed
// by `.askButton()`, unless it is a sheet, which says so with its own
// Close/Cancel/Done toolbar item (the screen underneath keeps the button).
func TestEveryPhonePageHasTheAskButton(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(appDir, "*.swift"))
	if err != nil || len(files) < 20 {
		t.Fatalf("app sources: %d files, %v", len(files), err)
	}
	pages := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(b), "\n")
		for i, l := range lines {
			if !strings.Contains(l, ".navigationTitle(") || strings.HasPrefix(strings.TrimSpace(l), "//") {
				continue
			}
			pages++
			near := strings.Join(lines[i:min(i+12, len(lines))], "\n")
			if strings.Contains(near, ".askButton()") || strings.Contains(near, "placement: .confirmationAction") || strings.Contains(near, "placement: .cancellationAction") {
				continue
			}
			t.Errorf("app/Life/Sources/%s:%d sets a navigationTitle with no .askButton() after it — add it (a sheet is exempt through its Close/Cancel toolbar item)", filepath.Base(f), i+1)
		}
	}
	if pages < 20 {
		t.Fatalf("only %d navigationTitle lines found — the scan has drifted from the app", pages)
	}
}

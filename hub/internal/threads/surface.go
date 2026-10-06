package threads

// Which surface an ask belongs to. Some asks are work for the laptop (a
// console link, a key to paste) and only clog the phone's list, so an ask
// carries WHERE it gets done:
//
//	mobile — the phone is the tool: install a build, tap an itms-services
//	         link, grant a permission in iOS Settings.
//	web    — the Mac is the tool: paste an API key, run an OAuth flow, drop a
//	         file, look at a console page. On the phone it counts and draws
//	         like any other card — only its console-route link is never a
//	         phone button.
//	any    — a decision, an answer to read, a real-world step. Either surface,
//	         so it counts on both. This is the default and the common case.
//
// The field is set explicitly (`lifectl ask add --surface web`) or inferred
// here from what the ask says, because most asks are written by agents that
// never think about it.

import (
	"strings"
	"time"
)

var askSurfaces = map[string]bool{"any": true, "mobile": true, "web": true}

// phoneWords: this is done by holding the phone.
var phoneWords = []string{"itms-services://", "install app build", "tap the link", "on your phone", "from your phone", "in the app"}

// laptopWords: this is done at the Mac. `:8443/#/` is a console route — the
// one link that is provably useless on the phone, since the console has no
// mobile layout at all.
var laptopWords = []string{":8443/#/", "on the mac", "on your mac", "at the mac", "on the laptop", "on your laptop", "web console", "the console", "ops/hub.sh web", "in the browser", "paste the key", "api key", "oauth"}

// InferSurface: where an ask gets done, from its own words. Phone first — an
// itms-services link is unambiguous and can co-occur with the hub's own host.
// Unsure is "any" on purpose: hiding an ask from the board is the expensive
// mistake, showing it on both is the cheap one.
func InferSurface(kind, title, detail string) string {
	// An install is done on the device it updates: the phone's card on the
	// phone, the desktop app's card at the Mac (2026-09-30).
	if kind == "install" {
		if installTarget(kind, title) == "mac" {
			return "web"
		}
		return "mobile"
	}
	t := strings.ToLower(title + "\n" + detail)
	for _, w := range phoneWords {
		if strings.Contains(t, w) {
			return "mobile"
		}
	}
	for _, w := range laptopWords {
		if strings.Contains(t, w) {
			return "web"
		}
	}
	// An "access" ask with a link is a credential to install: keys, tokens and
	// OAuth all live in the browser.
	if kind == "access" && strings.Contains(t, "https://") {
		return "web"
	}
	return "any"
}

// normSurface: "" (unset) means infer; a bad value is not an error worth
// failing an ask over — an ask that exists on both surfaces is never wrong,
// only noisier.
func normSurface(surface, kind, title, detail string) string {
	s := strings.ToLower(strings.TrimSpace(surface))
	switch s {
	case "phone", "ios", "app":
		s = "mobile"
	case "console", "laptop", "mac", "desktop":
		s = "web"
	case "both", "either":
		s = "any"
	}
	if askSurfaces[s] {
		return s
	}
	return InferSurface(kind, title, detail)
}

// SetAskSurface retags an existing ask (`lifectl ask <id> surface web`) — the
// escape hatch for a bad inference, and how an agent moves its own card to the
// laptop after the fact.
func (m *Manager) SetAskSurface(id, surface string) (Ask, error) {
	a, err := m.GetAsk(id)
	if err != nil {
		return Ask{}, err
	}
	s := normSurface(surface, a.Kind, a.Title, a.Detail)
	if _, err := m.db.Exec(`UPDATE items SET surface=?, updated_at=? WHERE id=?`, s, ts(time.Now()), id); err != nil {
		return Ask{}, err
	}
	return m.GetAsk(id)
}

// backfillAskSurface: every ask written before the column existed says "any",
// so the separation would only apply to new ones and the board the owner is
// looking at right now would not change. Runs once per boot over the ACTIVE rows only
// (closed ones are history and nothing reads their surface).
func (m *Manager) backfillAskSurface() {
	rows, err := m.db.Query(`SELECT id, kind, title, detail FROM items WHERE src='ask' AND surface='any' AND state IN ('open','answered')`)
	if err != nil {
		return
	}
	type row struct{ id, s string }
	var todo []row
	for rows.Next() {
		var id, kind, title, detail string
		if rows.Scan(&id, &kind, &title, &detail) != nil {
			continue
		}
		if s := InferSurface(kind, title, detail); s != "any" {
			todo = append(todo, row{id, s})
		}
	}
	rows.Close()
	for _, r := range todo {
		m.db.Exec(`UPDATE items SET surface=? WHERE id=?`, r.s, r.id)
	}
}

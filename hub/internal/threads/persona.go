package threads

import (
	"slices"
	"sort"
	"strings"
)

// Team mode (config team_mode, Manager.TeamMode): a long-lived session can
// play a named role. The role is a key in `personas`; the session's title is
// its name, whatever the owner calls it; its chat is its history; its
// workspace (internal/workspace) is its own files. Each turn of such a
// session opens with the persona block below, the router (route.go) sends a
// new message in its lane to it, and the other sessions see it in their
// peers header with its lane.
//
// One role ships, the executive assistant who owns the calendar. An owner's
// agent adds a role for the owner by adding a `personas` row (Role, Covers,
// Who, Lane, Brief), a `routeLanes` entry (route.go) and giving a session
// the key: `lifectl thread <id> persona <key>`. Every brief is written for
// the owner it serves, in the owner's words, never copied from anyone's.
type Persona struct {
	// Role: the two words under the name on the session's card at the foot
	// of the Sessions list.
	Role string
	// Covers: the lane in a few words, for the roster the routed note lists
	// under each role's name (routedNote).
	Covers string
	// Who: the role, as the other sessions and the brief name it.
	Who string
	// Lane: what the other sessions relay to it (peersHeader).
	Lane string
	// Brief: what the role holds, after "[You are <name>, <Who>: ".
	Brief string
}

const PersonaAssistant = "assistant"

var personas = map[string]Persona{
	PersonaAssistant: {
		Role:   "Executive assistant",
		Covers: "the owner's calendar, schedule and log, the home and plans with friends",
		Who:    "the owner's executive assistant",
		Lane:   "relay it anything about scheduling, the home or the owner's friends: a meeting, a booking, a time to find, a dated errand, a household purchase or repair, a plan with a friend or family",
		Brief:  "the one long-lived session behind the owner's calendar, their home and their time with friends. Every step and chore no other session owns fires here, and the owner's words on them come to you; other sessions relay you scheduling (meetings, bookings, finding a time, dated errands). The home is yours too (groceries and household orders, a repair, the apartment), and so are plans with friends and family. Anything nobody else on the team owns is yours to take or hand on. Keep the calendar true, chase what is due, and hand work that belongs elsewhere to the session that owns it with `lifectl relay <id> \"…\"` (the hub delivers it). Contacting anyone outside stays `lifectl propose --kind contact`.",
	},
}

// personaOrder: the roles' cards top to bottom, left to right. A key not
// listed here sorts last, by key.
var personaOrder = []string{PersonaAssistant}

func personaNames() []string {
	names := make([]string, 0, len(personas))
	for k := range personas {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// persona: the block that heads every turn of a session with a role.
func persona(t Thread) string { return personaWith(t, nil) }

// personaWith is persona with the names the session went by before
// (formerTitles): the chat behind it, its notes and files may still use one.
func personaWith(t Thread, former []string) string {
	p, ok := personas[t.Persona]
	if !ok {
		return ""
	}
	who := p.Who
	if t.Title != "" && t.Title != "Calendar" {
		who = t.Title + ", " + who
		if len(former) > 0 {
			old := strings.Join(former, ", then ")
			who = t.Title + " (formerly " + old + ": the owner renamed you; earlier messages, notes and files that say " + old + " mean you; answer to " + t.Title + " now), " + p.Who
		}
	}
	return "[You are " + who + ": " + p.Brief + " " + workspaceBrief(t.Persona) + " The team's names are its roles' titles in the session list each turn; the owner renames them, so call each by the name there, never one from older chat.]"
}

// formerTitles: the names a session went by before its title (oldest first).
func (m *Manager) formerTitles(id string) []string {
	var s string
	m.db.QueryRow(`SELECT former_titles FROM threads WHERE id=?`, id).Scan(&s)
	return splitFormer(s)
}

func splitFormer(s string) []string {
	var out []string
	for _, n := range strings.Split(s, "|") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// addFormerTitle records old as a name the session went by, once, and drops
// now from the list (a name given back is current again).
func (m *Manager) addFormerTitle(id, old, now string) {
	var keep []string
	for _, n := range append(m.formerTitles(id), old) {
		if n != now && n != "Calendar" && !slices.Contains(keep, n) {
			keep = append(keep, n)
		}
	}
	m.db.Exec(`UPDATE threads SET former_titles=? WHERE id=?`, strings.Join(keep, "|"), id)
}

// aka: a title with the names it replaced, for another session to read.
func aka(title string, former []string) string {
	if len(former) == 0 {
		return title
	}
	return title + " (formerly " + strings.Join(former, ", then ") + ")"
}

// workspaceBrief: the lines of a role's brief about its own folder. Shared
// facts stay goal notes; the workspace is what only this role would keep.
func workspaceBrief(key string) string {
	return "YOUR WORKSPACE is your own memory at a larger scale than the goal notes: files only you write and every session can read (data/people/" + key + "/): what you have learned about your lane, the state of each thing you run, analyses, working tables, drafts. " +
		"`lifectl workspace " + key + "` lists yours (first, whenever a task needs what you knew before); `lifectl workspace cat " + key + "/<name>` reads one; `lifectl workspace put <name> @file` writes one (markdown; a rewrite keeps the old version). " +
		"Keep it current: a durable thing you learn or decide on your lane goes in the file it belongs to THAT turn, under a name that says what it holds (README.md = what is here and how your lane works). Facts every session needs still go to the goal notes; another role's files are `lifectl workspace <role>`."
}

// IsPersona: is key a role (`personas`)?
func IsPersona(key string) bool { _, ok := personas[key]; return ok }

// RoleOf: the card words for a role key, "" for none.
func RoleOf(key string) string { return personas[key].Role }

// Personas: every session that plays a role, archived ones aside: the
// people at the foot of the Sessions list, whether or not one is calling.
// In role order, never by last message, so the cards hold still. Empty with
// team mode off, whatever the rows say.
func (m *Manager) Personas() ([]Thread, error) {
	if !m.TeamMode {
		return []Thread{}, nil
	}
	out, err := m.list("WHERE t.persona != '' AND t.status != 'archived'")
	rank := func(p string) int {
		for i, k := range personaOrder {
			if k == p {
				return i
			}
		}
		return len(personaOrder)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := rank(out[i].Persona), rank(out[j].Persona); a != b {
			return a < b
		}
		return out[i].Persona < out[j].Persona
	})
	return out, err
}

// PersonaOf: the role a session plays, "" for none (workspace.Store.Role).
func (m *Manager) PersonaOf(id string) string {
	if !m.TeamMode {
		return ""
	}
	var p string
	m.db.QueryRow(`SELECT persona FROM threads WHERE id=? AND status != 'archived'`, id).Scan(&p)
	return p
}

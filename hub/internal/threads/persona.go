package threads

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// Team mode (config team_mode, Manager.TeamMode): a long-lived session can
// play a named role. The role is a key (builtinPersonas, or a persona_defs
// row the owner's agent added); the session's title is its name, whatever the
// owner calls it; its chat is its history; its workspace (internal/workspace)
// is its own files. Each turn of such a session opens with the persona block
// below, the router (route.go) sends a new message in its lane to it, and the
// other sessions see it in their peers header with its lane.
//
// One role ships, the chief of staff: the owner's first stop, the router's
// catch-all and, until another role takes it, the calendar's session. A new
// role is made by asking the chief of staff, who agrees its words with the
// owner and runs `lifectl persona new` (POST /api/v1/personas): a
// persona_defs row and a session that plays it. Every brief is written for
// the owner it serves, in the owner's words, never copied from anyone's.
type Persona struct {
	Key string `json:"key"`
	// Role: the two words under the name on the session's card at the foot
	// of the Sessions list.
	Role string `json:"role"`
	// Covers: the lane in a few words, for the roster the routed note lists
	// under each role's name (routedNote).
	Covers string `json:"covers"`
	// Who: the role, as the other sessions and the brief name it.
	Who string `json:"who"`
	// Lane: what the other sessions relay to it (peersHeader).
	Lane string `json:"lane"`
	// RouteLane: the lane as the router's short prompt names it: concrete
	// words a small model can match, questions as much as changes. "" = the
	// router offers the role's session but names no lane for it.
	RouteLane string `json:"route_lane"`
	// Brief: what the role holds, after "[You are <name>, <Who>: ".
	Brief string `json:"brief"`
	// Team: the owner's name for the group the role sits in ("" = none).
	Team string `json:"team"`
	// Builtin: shipped with the hub (its text may still be rewritten).
	Builtin bool `json:"builtin"`
	// Session: the live session that plays it, "" for none (PersonaDefs).
	Session string `json:"session"`
}

const (
	// PersonaStaff: the chief of staff, the one role a fresh install has.
	PersonaStaff = "staff"
	// PersonaAssistant: the executive assistant an earlier release shipped;
	// kept so an install whose calendar session plays it keeps working.
	PersonaAssistant = "assistant"
)

const calendarLane = "anything on or about the owner's calendar, schedule or log, changing it or asking about it: add, move or cancel an appointment (a haircut, the dentist, a doctor, a meeting), a plan with someone on a day or at a time, a dated errand, a reminder; what is on the calendar, when something is set or due, when the owner last did or logged a thing and how many days it has been; a message that names a day and a time (Friday at 8, tomorrow at noon) is almost always this one's"

var builtinPersonas = map[string]Persona{
	PersonaStaff: {
		Role:      "Chief of staff",
		Covers:    "the owner's calendar and schedule, anything no other role owns, and the team itself",
		Who:       "the owner's chief of staff",
		Lane:      "relay it anything no other role owns, anything about the team (a new role, a renamed one), and anything about scheduling while no other role holds the calendar: a meeting, a booking, a time to find, a dated errand",
		RouteLane: calendarLane + "; a new role for the team, or who on the team does what",
		Brief:     "the owner's first stop. A new message no other role owns comes to you, and you are the one long-lived session behind the owner's calendar until the owner gives it to another role: every step and chore no other session owns fires here, and the owner's words on them come to you. Keep the calendar true, chase what is due, and hand work that belongs elsewhere to the session that owns it with `lifectl relay <id> \"…\"` (the hub delivers it). You also run the team. When the owner wants a new role (a coach, a bookkeeper, a researcher), agree its name, its lane and its brief with them in their own words, then make it with `lifectl persona new <key> --name \"<its name>\" --role \"<two words>\" --who \"…\" --covers \"…\" --lane \"…\" --brief @file`; it gets a session of its own. `lifectl personas` lists the team; `lifectl persona <key> set <field> <value>` rewrites one field. The owner names their own sections: `lifectl persona <key> set team \"<group>\"` groups roles, `lifectl goal <id> set section \"<group>\"` groups goals. Contacting anyone outside stays `lifectl propose --kind contact`.",
	},
	PersonaAssistant: {
		Role:      "Executive assistant",
		Covers:    "the owner's calendar, schedule and log, the home and plans with friends",
		Who:       "the owner's executive assistant",
		Lane:      "relay it anything about scheduling, the home or the owner's friends: a meeting, a booking, a time to find, a dated errand, a household purchase or repair, a plan with a friend or family",
		RouteLane: calendarLane + "; the home (groceries, a household order or repair, the apartment) and plans with friends or family",
		Brief:     "the one long-lived session behind the owner's calendar, their home and their time with friends. Every step and chore no other session owns fires here, and the owner's words on them come to you; other sessions relay you scheduling (meetings, bookings, finding a time, dated errands). The home is yours too (groceries and household orders, a repair, the apartment), and so are plans with friends and family. Anything nobody else on the team owns is yours to take or hand on. Keep the calendar true, chase what is due, and hand work that belongs elsewhere to the session that owns it with `lifectl relay <id> \"…\"` (the hub delivers it). Contacting anyone outside stays `lifectl propose --kind contact`.",
	},
}

// personaOrder: the roles' cards top to bottom, left to right. A key not
// listed here sorts after them, by key.
var personaOrder = []string{PersonaStaff, PersonaAssistant}

func personaRank(key string) int {
	for i, k := range personaOrder {
		if k == key {
			return i
		}
	}
	return len(personaOrder)
}

// validPersonaKey: a role's key is also its workspace folder name.
var validPersonaKey = regexp.MustCompile(`^[a-z][a-z0-9-]{1,23}$`)

// persona: a role as it stands: its newest persona_defs row, else the
// shipped text.
func (m *Manager) persona(key string) (Persona, bool) {
	if key == "" {
		return Persona{}, false
	}
	var p Persona
	err := m.db.QueryRow(`SELECT key, role, covers, who, lane, route_lane, brief, team FROM persona_defs WHERE key=? ORDER BY id DESC LIMIT 1`, key).
		Scan(&p.Key, &p.Role, &p.Covers, &p.Who, &p.Lane, &p.RouteLane, &p.Brief, &p.Team)
	_, builtin := builtinPersonas[key]
	if err != nil {
		if !builtin {
			return Persona{}, false
		}
		p = builtinPersonas[key]
		p.Key = key
	}
	p.Builtin = builtin
	return p, true
}

// IsPersona: is key a role?
func (m *Manager) IsPersona(key string) bool { _, ok := m.persona(key); return ok }

// RoleOf: the card words for a role key, "" for none.
func (m *Manager) RoleOf(key string) string { p, _ := m.persona(key); return p.Role }

// personaNames: every role key, sorted.
func (m *Manager) personaNames() []string {
	seen := map[string]bool{}
	for k := range builtinPersonas {
		seen[k] = true
	}
	if rows, err := m.db.Query(`SELECT DISTINCT key FROM persona_defs`); err == nil {
		for rows.Next() {
			var k string
			if rows.Scan(&k) == nil {
				seen[k] = true
			}
		}
		rows.Close()
	}
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// personaSession: the live session that plays key, "" for none.
func (m *Manager) personaSession(key string) string {
	var id string
	m.db.QueryRow(`SELECT id FROM threads WHERE persona=? AND status != 'archived' ORDER BY created_at LIMIT 1`, key).Scan(&id)
	return id
}

// PersonaDefs: every role in role order, each with the live session that
// plays it ("" for none): `lifectl personas`, GET /api/v1/personas.
func (m *Manager) PersonaDefs() []Persona {
	out := []Persona{}
	for _, k := range m.personaNames() {
		if p, ok := m.persona(k); ok {
			p.Session = m.personaSession(k)
			if k == PersonaAssistant && p.Builtin && p.Session == "" {
				continue // an older install's role: listed only where a session still plays it
			}
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := personaRank(out[i].Key), personaRank(out[j].Key); a != b {
			return a < b
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func (m *Manager) writePersona(p Persona, by string) error {
	_, err := m.db.Exec(`INSERT INTO persona_defs (key, role, covers, who, lane, route_lane, brief, team, by, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		p.Key, p.Role, p.Covers, p.Who, p.Lane, p.RouteLane, p.Brief, p.Team, by, ts(time.Now()))
	return err
}

// NewPersona adds a role and a session that plays it, titled name (the
// role's words when empty). The key must be new; role, who and brief are
// required, covers and lane default from each other.
func (m *Manager) NewPersona(p Persona, name, by string) (Persona, error) {
	if !m.TeamMode() {
		return Persona{}, errTeamOff
	}
	p.Key = strings.TrimSpace(p.Key)
	if !validPersonaKey.MatchString(p.Key) {
		return Persona{}, errors.New("key: 2-24 lowercase letters, digits or -, starting with a letter")
	}
	if m.IsPersona(p.Key) {
		return Persona{}, fmt.Errorf("%s is already a role (`lifectl persona %s set <field> <value>` changes it)", p.Key, p.Key)
	}
	for _, f := range []*string{&p.Role, &p.Covers, &p.Who, &p.Lane, &p.RouteLane, &p.Brief, &p.Team} {
		*f = strings.TrimSpace(*f)
	}
	if p.Role == "" || p.Who == "" || p.Brief == "" {
		return Persona{}, errors.New("role, who and brief are required")
	}
	if p.Covers == "" {
		p.Covers = p.Lane
	}
	if p.Lane == "" && p.Covers != "" {
		p.Lane = "relay it anything about " + p.Covers
	}
	if name = strings.TrimSpace(name); name == "" {
		name = p.Role
	}
	if err := m.writePersona(p, by); err != nil {
		return Persona{}, err
	}
	now := time.Now()
	id := slug(name)
	if _, err := m.db.Exec(`INSERT INTO threads (id,created_at,updated_at,title,project,status,schedule,schedule_prompt,persona,title_auto,model_class) VALUES (?,?,?,?,'life','done','','',?,0,?)`,
		id, ts(now), ts(now), name, p.Key, m.DefaultModel()); err != nil {
		return Persona{}, err
	}
	m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text) VALUES (?,?,?,?,?)`, id, ts(now), "system", "schedule",
		"This session plays a role on the team: "+p.Role+".")
	p, _ = m.persona(p.Key)
	p.Session = id
	return p, nil
}

// personaFields: what `lifectl persona <key> set` may rewrite.
var personaFields = []string{"role", "covers", "who", "lane", "route_lane", "brief", "team"}

// EditPersona rewrites fields of a role (a built-in one too) as a new
// persona_defs row; the next turn of its session reads the new words.
func (m *Manager) EditPersona(key string, patch map[string]string, by string) (Persona, error) {
	p, ok := m.persona(key)
	if !ok {
		return Persona{}, fmt.Errorf("no role %q (one of %s)", key, strings.Join(m.personaNames(), "|"))
	}
	for k, v := range patch {
		v = strings.TrimSpace(v)
		switch k {
		case "role":
			p.Role = v
		case "covers":
			p.Covers = v
		case "who":
			p.Who = v
		case "lane":
			p.Lane = v
		case "route_lane":
			p.RouteLane = v
		case "brief":
			p.Brief = v
		case "team":
			p.Team = v
		default:
			return Persona{}, fmt.Errorf("%s: not a role field (%s)", k, strings.Join(personaFields, "|"))
		}
	}
	if p.Role == "" || p.Who == "" || p.Brief == "" {
		return Persona{}, errors.New("role, who and brief cannot be emptied")
	}
	if err := m.writePersona(p, by); err != nil {
		return Persona{}, err
	}
	p, _ = m.persona(key)
	p.Session = m.personaSession(key)
	return p, nil
}

// CatchAll: the session a message no role claims goes to while plain
// sessions are off: the chief of staff, else an older install's executive
// assistant. "" for none.
func (m *Manager) CatchAll() string {
	for _, key := range []string{PersonaStaff, PersonaAssistant} {
		if id := m.personaSession(key); id != "" {
			return id
		}
	}
	return ""
}

var errTeamOff = errors.New("team mode is off: the owner turns it on (`lifectl settings set team_mode true`, PATCH /api/v1/settings), then try again")

// TeamMode: are roles, the router and workspaces on (config team_mode)?
func (m *Manager) TeamMode() bool { return m.team.Load() }

// PlainSessions: with team mode on, may a new message from an app start a
// session with no role (config plain_sessions)? Off, a message no role
// claims goes to the CatchAll.
func (m *Manager) PlainSessions() bool { return m.plain.Load() }

// SetPlainSessions applies plain_sessions now.
func (m *Manager) SetPlainSessions(on bool) { m.plain.Store(on) }

// SetTeamMode applies team_mode now (the switch alone; StartTeam does the
// rest of turning it on).
func (m *Manager) SetTeamMode(on bool) { m.team.Store(on) }

// StartTeam turns team mode on with what it needs: the calendar's home
// session gets the chief of staff if it plays no role yet (made here when it
// does not exist: on a database that predates the switch the calendar makes
// it only when a step first fires), and the router's local model is kept warm
// from then on (WarmLocal, started once, only with WarmRouter set).
func (m *Manager) StartTeam() {
	m.team.Store(true)
	if _, err := m.Get(StepThread); err != nil {
		if _, err := m.CreateIdle(StepThread, "Calendar", "life"); err != nil {
			log.Printf("calendar thread: %v", err)
		}
	} else {
		m.db.Exec(`UPDATE threads SET persona=? WHERE id=? AND persona='' AND status != 'archived'`, PersonaStaff, StepThread)
	}
	if m.Local != nil && m.WarmRouter {
		m.warmOnce.Do(func() { go m.WarmLocal() })
	}
}

// personaWith: the block that heads every turn of a session with a role,
// with the names the session went by before (formerTitles): the chat behind
// it, its notes and files may still use one.
func (m *Manager) personaWith(t Thread, former []string) string {
	p, ok := m.persona(t.Persona)
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

// Personas: every session that plays a role, archived ones aside: the
// people at the foot of the Sessions list, whether or not one is calling.
// In role order, never by last message, so the cards hold still. Empty with
// team mode off, whatever the rows say.
func (m *Manager) Personas() ([]Thread, error) {
	if !m.TeamMode() {
		return []Thread{}, nil
	}
	out, err := m.list("WHERE t.persona != '' AND t.status != 'archived'")
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := personaRank(out[i].Persona), personaRank(out[j].Persona); a != b {
			return a < b
		}
		return out[i].Persona < out[j].Persona
	})
	return out, err
}

// PersonaOf: the role a session plays, "" for none (workspace.Store.Role).
func (m *Manager) PersonaOf(id string) string {
	if !m.TeamMode() {
		return ""
	}
	var p string
	m.db.QueryRow(`SELECT persona FROM threads WHERE id=? AND status != 'archived'`, id).Scan(&p)
	return p
}

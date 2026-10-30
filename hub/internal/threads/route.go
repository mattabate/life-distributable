package threads

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Routing a new session (team mode): a message the owner starts a NEW
// session with from an app (the New session box) goes first past a router,
// which may hand it to a session that already holds that work instead: a
// role whose lane covers it, a live session, or one of the last few stopped
// ones. A message typed into a session the owner clicked into is never
// routed, and a new session is always a valid answer (a fundamentally
// different task deserves its own). lifectl and the hub's own sessions never
// route; only the owner's surfaces call CreateRouted, and only while
// Manager.TeamMode is on.
//
// The router is a local model first (Ollama, when it is listening) and a
// cheap Claude model second; neither is required: with no router at all a
// new message is a new session, as it is with team mode off.

// routeRecent: the stopped sessions offered beside the live ones and the
// roles.
const routeRecent = 5

// routeTimeout: past this the message just starts a new session; a slow
// router must never hold the owner's words.
const routeTimeout = 25 * time.Second

// localTimeout: the local model's cap, generous enough for a model paged
// back in from disk after hours idle (a cold route is about 12 s on a Mac
// mini). A timeout means a new session, never the cloud model on top.
const localTimeout = 15 * time.Second

// warmEvery: how often the hub touches the local model so its weights stay
// resident; a long keep_alive alone leaves the loaded model cold after hours
// of idle.
const warmEvery = 5 * time.Minute

// routeCandidate: one session the router may pick, with what it is told
// about it.
type routeCandidate struct {
	ID, Block string
	// Short: the same in a few lines; the local model's prefill is its
	// time, so it reads each session's job or title and its last two lines.
	Short string
	// Persona: the role's key for a session with a role, else "".
	Persona string
}

// routeLanes: each role's lane as the short prompt's rules name it, by role
// key: concrete words a small model can match. A lane covers questions as
// much as changes. An owner's agent adding a role adds its lane here, next
// to its `personas` row.
var routeLanes = map[string]string{
	PersonaAssistant: "anything on or about the owner's calendar, schedule or log, changing it or asking about it: add, move or cancel an appointment (a haircut, the dentist, a doctor, a meeting), a plan with someone on a day or at a time, a dated errand, a reminder; what is on the calendar, when something is set or due, when the owner last did or logged a thing and how many days it has been; a message that names a day and a time (Friday at 8, tomorrow at noon) is almost always this one's; the home (groceries, a household order or repair, the apartment) and plans with friends or family",
}

// RouteLane: a role the local model may name by its key (the word
// "assistant", not a session hash) and the session behind it.
type RouteLane struct{ Key, ID string }

// routedNote heads a message the router sent to a session that did not start
// it (CreateRouted), or one such a session passed on to its real owner
// (Queue). Both begin with shown.go's routedMark, so both surfaces fold the
// note to the one "routed" tag. The roster in it is built from the roles'
// titles now, so a rename reaches the next routed message with no edit here.
func (m *Manager) routedNote(why string) string {
	var roster []string
	if ps, err := m.Personas(); err == nil {
		for _, t := range ps {
			p := personas[t.Persona]
			name := strings.ToLower(p.Role[:1]) + p.Role[1:]
			if t.Title != "" && t.Title != "Calendar" {
				name = aka(t.Title, m.formerTitles(t.ID)) + ", the " + name
			} else {
				name = "the " + name
			}
			roster = append(roster, fmt.Sprintf("%s (%s): %s", name, t.ID, p.Covers))
		}
	}
	return fmt.Sprintf("[Routed here: the owner started a NEW session with this message and the hub's router sent it to this session instead (%s). It is their message to you. The router is not a session and the owner never hears from it: if this is another role's lane (the team: %s) or work another session holds, do NOT answer it; pass the owner's words on exactly as written with `lifectl prompt \"<their words>\" --thread <that session's id>` (`--new` if nothing owns it) and end this turn with `[end]`: no card, no reply; they hear from the session that owns it.]\n\n", why, strings.Join(roster, "; "))
}

func passedOnNote(from string) string {
	return fmt.Sprintf("[Routed here: the owner's message, passed on word for word by session %s, where the hub's router had first sent it. It is their message to you; answer them here.]\n\n", from)
}

// CreateRouted is CreateVia behind the router: the session that took the
// message (an existing one, or the new one), and whether it was routed.
func (m *Manager) CreateRouted(prompt string, attachments []string, via string) (Thread, bool, error) {
	id, why := m.Route(prompt)
	if id != "" {
		note := m.routedNote(why)
		if err := m.SendVia(id, note+prompt, attachments, via); err == nil {
			t, err := m.Get(id)
			return t, true, err
		} else {
			log.Printf("route: %s refused the message (%v); a new session takes it", id, err)
		}
	}
	t, err := m.CreateVia("", "life", "", prompt, "", "", attachments, via)
	return t, false, err
}

// passesOn: whether text is, word for word, a message the owner sent session
// `from` (the hub's bracket notes around it aside), which lets that session
// hand it to another without the owner's approval (Queue).
func (m *Manager) passesOn(from, text string) bool {
	want := oneLine(stripBrackets(text), 1<<20)
	if want == "" {
		return false
	}
	ms, err := m.Messages(from, 30)
	if err != nil {
		return false
	}
	for _, msg := range ms {
		if msg.Role == "owner" && oneLine(stripBrackets(msg.Text), 1<<20) == want {
			return true
		}
	}
	return false
}

// Route asks the router where a new-session message belongs: a candidate's
// id and the router's reason, or "" for a new session (team mode off, no
// candidates, no router, a timeout, an answer that is not a candidate).
func (m *Manager) Route(prompt string) (string, string) { return m.RouteWith(prompt, "") }

// RouteWith is Route with another Ollama model in place of localModel (""
// = localModel): the dry run's way to compare models without a restart.
func (m *Manager) RouteWith(prompt, model string) (string, string) {
	if !m.TeamMode {
		return "", ""
	}
	local := m.Local
	if model == "" {
		model = localModel
	} else if local != nil {
		local = func(p string, lanes []RouteLane, ids []string) (string, error) {
			return ollamaPickWith(model, p, lanes, ids)
		}
	}
	if strings.TrimSpace(prompt) == "" {
		return "", ""
	}
	if id, name := m.addressedPersona(prompt); id != "" {
		log.Printf("route: %s (opened by name: %s)", id, name)
		return id, "the owner opened the message by name: " + name
	}
	if m.Pick == nil && local == nil {
		return "", ""
	}
	cands := m.routeCandidates()
	if len(cands) == 0 {
		return "", ""
	}
	start := time.Now()
	// The local model first (the cloud model through the CLI takes 5-12 s,
	// mostly the CLI starting); the cloud model only when the local one is
	// not there or fails.
	if local != nil {
		ids := make([]string, len(cands))
		var lanes []RouteLane
		for i, c := range cands {
			ids[i] = c.ID
			if routeLanes[c.Persona] != "" {
				lanes = append(lanes, RouteLane{c.Persona, c.ID})
			}
		}
		if out, err := local(routePrompt(prompt, cands, true), lanes, ids); err == nil {
			id, why := parseRoute(out)
			for _, c := range cands {
				if c.ID == id {
					log.Printf("route: %s in %s (local %s): %s", id, time.Since(start).Round(time.Millisecond), model, why)
					return id, why
				}
			}
			log.Printf("route: new session in %s (local %s, %q)", time.Since(start).Round(time.Millisecond), model, firstLine(out, 120))
			return "", ""
		} else if errors.Is(err, context.DeadlineExceeded) {
			// The words have waited localTimeout already; the cloud model's
			// seconds on top is the one outcome worse than a new session.
			log.Printf("route: local model timed out after %s; new session", time.Since(start).Round(time.Millisecond))
			return "", ""
		} else {
			log.Printf("route: local model failed after %s (%v); cloud model", time.Since(start).Round(time.Millisecond), err)
		}
	}
	if m.Pick == nil {
		return "", ""
	}
	out, err := m.Pick(routePrompt(prompt, cands, false))
	if err != nil {
		log.Printf("route: cloud model failed after %s (%v); new session", time.Since(start).Round(time.Millisecond), err)
		return "", ""
	}
	id, why := parseRoute(out)
	for _, c := range cands {
		if c.ID == id {
			log.Printf("route: %s in %s: %s", id, time.Since(start).Round(time.Millisecond), why)
			return id, why
		}
	}
	log.Printf("route: new session in %s (%q)", time.Since(start).Round(time.Millisecond), firstLine(out, 120))
	return "", ""
}

// addressedPersona: the role the owner opens a message to by name ("Uh,
// Dana, um, so here's the deal...", "Hey, Dana, what is this?"), whose
// session takes it with no model asked. Only an opening counts: past filler
// words ("uh", "hey", "so"), a name with a comma or other mark after it, or
// straight after a greeting. A name later on ("Something seems odd with
// Dana") is about that role, not to it, and the router decides. Only a
// one-word title is a name; the calendar session's default title is not.
func (m *Manager) addressedPersona(prompt string) (string, string) {
	ps, err := m.Personas()
	if err != nil {
		return "", ""
	}
	byName := map[string]Thread{}
	for _, t := range ps {
		if n := strings.ToLower(strings.TrimSpace(t.Title)); n != "" && n != "calendar" && !strings.Contains(n, " ") {
			byName[n] = t
		}
	}
	if len(byName) == 0 {
		return "", ""
	}
	s := strings.TrimSpace(stripBrackets(stripSnapPreamble(prompt)))
	greeted := false
	for i := 0; i < 6; i++ {
		end := strings.IndexFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' })
		word, rest := s, ""
		if end >= 0 {
			word, rest = s[:end], s[end:]
		}
		if word == "" {
			return "", ""
		}
		w := strings.ToLower(word)
		r, _ := utf8.DecodeRuneInString(strings.TrimLeft(rest, " "))
		if t, ok := byName[w]; ok && (strings.ContainsRune(",:;!?.—-", r) || greeted) {
			return t.ID, t.Title
		}
		if !addressFillers[w] {
			return "", ""
		}
		greeted = greeted || w == "hey" || w == "hi" || w == "hello" || w == "yo"
		s = strings.TrimLeft(rest, " ,.;:!?—-")
	}
	return "", ""
}

// addressFillers: what is said before a name when speaking to someone.
var addressFillers = map[string]bool{"uh": true, "um": true, "umm": true, "hey": true, "hi": true, "hello": true, "yo": true, "ok": true, "okay": true, "so": true, "oh": true, "alright": true, "and": true, "well": true}

// parseRoute reads "<id> | <reason>" from the first non-empty line.
func parseRoute(out string) (string, string) {
	for _, l := range strings.Split(out, "\n") {
		l = strings.Trim(strings.TrimSpace(l), "`*")
		if l == "" {
			continue
		}
		id, why, _ := strings.Cut(l, "|")
		return strings.Trim(strings.TrimSpace(id), "`*\"'"), strings.TrimSpace(why)
	}
	return "", ""
}

// routeCandidates: every role with its job and the tail of its chat, every
// live session (working or waiting on the owner), and the routeRecent
// stopped ones.
func (m *Manager) routeCandidates() []routeCandidate {
	var out []routeCandidate
	seen := map[string]bool{}
	add := func(ts []Thread, kind string) {
		for _, t := range ts {
			if seen[t.ID] || t.ID == "hub" {
				continue
			}
			seen[t.ID] = true
			out = append(out, routeCandidate{t.ID, m.routeBlock(t, kind, false), m.routeBlock(t, kind, true), t.Persona})
		}
	}
	if ps, err := m.Personas(); err == nil {
		add(ps, "persona")
	}
	add(m.routeThreads(`status IN ('running','needs_you')`, 20), "live")
	add(m.routeThreads(`status IN ('done','idle')`, routeRecent), "recent")
	return out
}

// routeThreads: the newest-talked-to sessions without a role in a status,
// picked in plain SQL so List's per-row columns run on a handful.
func (m *Manager) routeThreads(status string, n int) []Thread {
	rows, err := m.db.Query(`SELECT t.id FROM threads t WHERE t.`+status+` AND t.persona = '' AND t.id != 'hub'
		ORDER BY COALESCE((SELECT MAX(ts) FROM thread_messages m WHERE m.thread_id=t.id), t.updated_at) DESC LIMIT ?`, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	var out []Thread
	for _, id := range ids {
		if t, err := m.Get(id); err == nil {
			out = append(out, t)
		}
	}
	return out
}

// routeBlock: what the router reads about one session: its name, its job if
// it has a role, and the last few messages (longer for a role, whose chat is
// its history).
func (m *Manager) routeBlock(t Thread, kind string, short bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n", t.ID)
	switch kind {
	case "persona":
		p := personas[t.Persona]
		name := t.Title
		if name == "" || name == "Calendar" {
			name = "(unnamed)"
		}
		if short {
			fmt.Fprintf(&b, "ROLE %s, %s: %s\n", name, p.Who, p.Lane)
		} else {
			fmt.Fprintf(&b, "ROLE %s, %s. Job: %s\nLane: %s\n", name, p.Who, p.Brief, p.Lane)
		}
	case "live":
		fmt.Fprintf(&b, "LIVE SESSION (%s): %s\n", map[string]string{"running": "working now", "needs_you": "waiting on the owner"}[t.Status], t.Title)
	default:
		fmt.Fprintf(&b, "RECENTLY STOPPED SESSION, last active %s: %s\n", t.UpdatedAt.In(time.Local).Format("Jan 2 15:04"), t.Title)
	}
	if t.GoalID != "" {
		fmt.Fprintf(&b, "Goal: %s\n", t.GoalID)
	}
	n, width := 4, 300
	if kind == "persona" {
		n, width = 8, 400
	}
	if short {
		n, width = 2, 160
	}
	if ms, err := m.Messages(t.ID, n); err == nil && len(ms) > 0 {
		b.WriteString("Last messages:\n")
		for i := len(ms) - 1; i >= 0; i-- {
			msg := ms[i]
			who := "agent"
			if msg.Role == "owner" {
				who = "owner"
			}
			fmt.Fprintf(&b, "- %s: %s\n", who, oneLine(stripBrackets(msg.Text), width))
		}
	}
	return b.String()
}

// stripBrackets drops the hub's [bracket] blocks from a message so the
// router reads the words, not the boilerplate around them.
func stripBrackets(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '[':
			depth++
		case r == ']' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func routePrompt(msg string, cands []routeCandidate, short bool) string {
	var b strings.Builder
	b.WriteString("You route a message the owner just sent from the \"new session\" box of their personal assistant app. Either it starts a NEW session, or it goes to ONE existing session below that already owns that work.\n\n")
	b.WriteString("Rules:\n- NEW is always valid and is the default: pick an existing session only when the message is clearly in its lane or continues its work (the same task, page, purchase or plan).\n- A session with a role takes ANY message in its lane, even a brand-new task (the executive assistant: anything to put on, change on or ask about the owner's calendar: appointments, errands, plans with people on a day, when they last did something).\n- The lane comes first: a session whose title or chat merely mentions the same subject (a session named after the calendar is not the calendar) never takes a message in a role's lane.\n- A live or recently stopped session takes a message that continues what it was doing.\n- A fundamentally different task, or one the owner wants kept apart, is NEW.\n\n")
	if short {
		b.WriteString("Lanes (answer the lane's key when the message is in it, whatever the sessions below say):\n")
		for _, c := range cands {
			if lane := routeLanes[c.Persona]; lane != "" {
				fmt.Fprintf(&b, "- lane \"%s\" (%s, session %s): %s.\n", c.Persona, personas[c.Persona].Who, c.ID, lane)
			}
		}
		b.WriteString("\nAnswer as JSON: {\"about\": \"<what the message is about, under 10 words>\", \"lane\": \"<lane key>\" or \"none\", \"session\": \"<session id>\" or \"new\"}; the lane first; the session only matters when the lane is none.\n\n# Sessions\n\n")
	} else {
		b.WriteString("Answer with ONE line and nothing else: `<session id> | <reason, under 15 words>` or `new | <reason>`.\n\n# Sessions\n\n")
	}
	for _, c := range cands {
		if short {
			b.WriteString(c.Short)
		} else {
			b.WriteString(c.Block)
		}
		b.WriteString("\n")
	}
	// The owner's words last: the prompt before them is the same from one
	// route to the next while the sessions are, so the local model reuses
	// its cache.
	b.WriteString("# The owner's message\n\n")
	limit := 3000
	if short {
		limit = 1200
	}
	b.WriteString(oneLine(stripBrackets(stripSnapPreamble(msg)), limit))
	b.WriteString("\n")
	return b.String()
}

// localModel: the router's model in Ollama, and where Ollama listens.
// Nothing listening = the cloud model answers instead (or, with none, a new
// session). Ollama is optional: `brew install ollama && ollama pull qwen3:8b`
// is all it takes to turn the local router on.
const (
	localModel = "qwen3:8b"
	ollamaURL  = "http://127.0.0.1:11434/api/chat"
)

// ollamaPick: the router's prompt to the local model, its answer held by a
// JSON schema (Ollama's structured output) to a lane key or "none" and then
// to one of the candidate ids or "new", returned as the same "<id> | <reason>"
// line the cloud model writes. The lane is asked first and settles it: the
// rule that a role takes ANY message in its lane is the hub's, not the
// model's weighing of a lane against a session that once heard the subject.
func ollamaPick(prompt string, lanes []RouteLane, ids []string) (string, error) {
	return ollamaPickWith(localModel, prompt, lanes, ids)
}

// ollamaOptions: the one set of runner options every call sends; a different
// num_ctx makes Ollama reload the model, so the warm-up and the route agree.
// num_ctx: the short prompt is a few thousand tokens; Ollama's 32k default
// holds far more memory for the same answers.
func ollamaOptions(predict int) map[string]any {
	return map[string]any{"temperature": 0, "num_predict": predict, "num_ctx": 8192}
}

func ollamaPickWith(model, prompt string, lanes []RouteLane, ids []string) (string, error) {
	laneKeys := []string{"none"}
	for _, l := range lanes {
		laneKeys = append(laneKeys, l.Key)
	}
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"stream":     false,
		"think":      false,
		"keep_alive": "24h",
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
		"format": map[string]any{
			"type": "object",
			"properties": map[string]any{
				// json.Marshal orders keys and Ollama's grammar follows that
				// order: "about" (the topic) before "lane" (a role's key or
				// none) before "session": a reason, the lane, then the id.
				"about":   map[string]any{"type": "string"},
				"lane":    map[string]any{"type": "string", "enum": laneKeys},
				"session": map[string]any{"type": "string", "enum": append([]string{"new"}, ids...)},
			},
			"required": []string{"about", "lane", "session"},
		},
		"options": ollamaOptions(60),
	})
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", ollamaURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Message struct{ Content string }
		Error   string
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", errors.New(out.Error)
	}
	var pick struct {
		Lane    string `json:"lane"`
		Session string `json:"session"`
		Reason  string `json:"about"`
	}
	if err := json.Unmarshal([]byte(out.Message.Content), &pick); err != nil {
		return "", fmt.Errorf("not json: %s", truncate(out.Message.Content, 120))
	}
	for _, l := range lanes {
		if l.Key == pick.Lane {
			return l.ID + " | lane " + l.Key + ": " + pick.Reason, nil
		}
	}
	return pick.Session + " | " + pick.Reason, nil
}

// WarmLocal keeps the router's local model hot: once now and every warmEvery,
// a one-token generation over the route prompt as it stands (the rules, the
// lanes and today's candidates, no message), so the runner's one slot keeps
// that whole prefix cached and the next real route prefills only the owner's
// words. Runs for the hub's life; nothing when there is no local router
// (tests, team mode off). The log hears only a change: a warm-up over 3 s
// (the model WAS cold, or just loaded), a failure, and the first success
// after one.
func (m *Manager) WarmLocal() {
	if m.Local == nil || !m.TeamMode {
		return
	}
	failing := false
	for {
		took, err := ollamaWarm(localModel, routePrompt("", m.routeCandidates(), true))
		switch {
		case err != nil && !failing:
			log.Printf("route: local model warm-up failed (%v); routes fall to the cloud model until it is back", err)
			failing = true
		case err == nil && failing:
			log.Printf("route: local model is back (warm in %s)", took.Round(time.Millisecond))
			failing = false
		case err == nil && took > 3*time.Second:
			log.Printf("route: local model was cold: warm in %s", took.Round(time.Millisecond))
		}
		time.Sleep(warmEvery)
	}
}

// ollamaWarm: one token from the local model over prompt, the same runner
// options as a route so Ollama keeps the loaded instance rather than
// reloading it.
func ollamaWarm(model, prompt string) (time.Duration, error) {
	start := time.Now()
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"stream":     false,
		"think":      false,
		"keep_alive": "24h",
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
		"options":    ollamaOptions(1),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", ollamaURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return time.Since(start), err
	}
	defer resp.Body.Close()
	var out struct{ Error string }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return time.Since(start), err
	}
	if out.Error != "" {
		return time.Since(start), errors.New(out.Error)
	}
	return time.Since(start), nil
}

// haikuPick: the router's one cloud call, tool-less from the side folder
// like the titles, cut off at routeTimeout.
func (m *Manager) haikuPick(prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), routeTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, m.ClaudeBin, "-p", "--model", "claude-haiku-4-5-20251001", "--output-format", "text", "--max-turns", "1", "--tools", "")
	c.Dir = sideDir()
	c.Stdin = strings.NewReader(prompt)
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, truncate(string(out), 200))
	}
	return string(out), nil
}

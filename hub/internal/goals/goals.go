// Package goals: the owner's goals as first-class objects. A goal is a
// statement plus scope; sessions read them as context and write notes back.
// Notes are append-only (they are the goal's history). No job reviews goals
// on a clock: agents should work and then say how the owner can help, not
// check in on their goals every few days.
package goals

import (
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"life/hub/internal/store"
)

// Schema: goals and their notes (store/migrate.go).
var Schema = []string{`CREATE TABLE IF NOT EXISTS goals (
	id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	title TEXT NOT NULL, statement TEXT NOT NULL DEFAULT '',
	horizon TEXT NOT NULL DEFAULT 'ongoing',   -- month | quarter | year | ongoing
	cadence TEXT NOT NULL DEFAULT 'weekly',    -- how often the review job looks at it
	status TEXT NOT NULL DEFAULT 'active',     -- active | paused | done
	sources TEXT NOT NULL DEFAULT '',          -- comma list: health, finance, spend, calendar…
	last_reviewed_at TEXT);
CREATE TABLE IF NOT EXISTS goal_notes (
	id INTEGER PRIMARY KEY, goal_id TEXT NOT NULL, created_at TEXT NOT NULL,
	author TEXT NOT NULL,                      -- owner | claude:<job>
	kind TEXT NOT NULL DEFAULT 'note',         -- note | review | suggestion | metric
	text TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS goal_notes_goal ON goal_notes(goal_id, id);`,
	// The digest is the goal's CURRENT STATE in flat prose: what is true, what
	// is open, what the next checkpoint is. Notes stay append-only history, but
	// a session should not have to re-read 20 of them (12-29 KB per goal, every
	// wake, in a context that is re-read on every turn) to learn where things
	// stand, so what is learned is copied into flat prose and stale text wiped.
	`ALTER TABLE goals ADD COLUMN digest TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE goals ADD COLUMN digest_at TEXT`}

type Goal struct {
	ID             string     `json:"id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Title          string     `json:"title"`
	Statement      string     `json:"statement"`
	Horizon        string     `json:"horizon"`
	Cadence        string     `json:"cadence"`
	Status         string     `json:"status"`
	Sources        string     `json:"sources"`
	LastReviewedAt *time.Time `json:"last_reviewed_at,omitempty"`
	NoteCount      int        `json:"note_count"`
	Digest         string     `json:"digest,omitempty"`
	DigestAt       *time.Time `json:"digest_at,omitempty"`
	// The goal's face on a page: a symbol and a hue both surfaces draw the
	// same way, so a goal card is never blank. Derived from the name on every read, never stored, so a
	// session that creates or renames a goal never has to pick one.
	Emblem Emblem `json:"emblem"`
}

// Emblem is a goal's symbol and hue. `Symbol` is one of the words in
// emblemStems (the console maps it to an inline SVG, the phone to an SF
// Symbol); `Hue` is a whole degree, 0..359.
type Emblem struct {
	Symbol string `json:"symbol"`
	Hue    int    `json:"hue"`
}

// emblemStems: the first stem that begins a word of the goal's slug names the
// symbol. Order matters — "commercialize the life agent" is a market before it
// is an agent, and "smarter" must never read as art (prefix on whole words).
var emblemStems = []struct {
	stem, symbol string
	hue          int
}{
	{"health", "health", 350}, {"fit", "health", 350}, {"sleep", "health", 350},
	{"commerc", "market", 265}, {"sell", "market", 265}, {"custom", "market", 265},
	{"money", "money", 150}, {"income", "money", 150}, {"wealth", "money", 150}, {"invest", "money", 150},
	{"agent", "agent", 220}, {"build", "agent", 220},
	{"audience", "audience", 25}, {"follow", "audience", 25}, {"reach", "audience", 25},
	{"smart", "learn", 195}, {"learn", "learn", 195}, {"stud", "learn", 195}, {"read", "learn", 195},
	{"art", "art", 320}, {"music", "art", 320}, {"write", "art", 320}, {"compos", "art", 320},
}

// EmblemFor names a goal's symbol and hue from its slug (or title). A goal no
// stem fits is a plain target with a hue hashed from its id, so it still gets
// a colour of its own and keeps it across renames of the statement.
func EmblemFor(id, title string) Emblem {
	words := strings.FieldsFunc(strings.ToLower(id+" "+title), func(r rune) bool {
		return r == '-' || r == ' ' || r == '_'
	})
	for _, s := range emblemStems {
		for _, w := range words {
			if strings.HasPrefix(w, s.stem) {
				return Emblem{Symbol: s.symbol, Hue: s.hue}
			}
		}
	}
	h := fnv.New32a()
	h.Write([]byte(id))
	return Emblem{Symbol: "goal", Hue: int(h.Sum32() % 360)}
}

type Note struct {
	ID        int64     `json:"id"`
	GoalID    string    `json:"goal_id"`
	CreatedAt time.Time `json:"created_at"`
	Author    string    `json:"author"`
	Kind      string    `json:"kind"`
	Text      string    `json:"text"`
	// Who wrote it, in words a page can print: the session's title for a
	// `claude:thread:<id>` author, "You" for the owner, else the author
	// with its `claude:` prefix dropped. Never a raw thread id or a kind pill:
	// the kind is a session's filing word, not the reader's.
	By string `json:"by"`
	// The session that wrote it, when it still exists — a link on the by-line.
	ThreadID string `json:"thread_id,omitempty"`
}

// byLine words an author for a page. A thread title beats its id; anything
// else is the author minus the machine prefix.
func byLine(author, threadTitle string) (by, threadID string) {
	switch {
	case author == "owner":
		return "You", ""
	case strings.HasPrefix(author, "claude:thread:"):
		id := strings.TrimPrefix(author, "claude:thread:")
		if threadTitle != "" {
			return threadTitle, id
		}
		return id, ""
	case author == "claude":
		return "Claude", ""
	case strings.HasPrefix(author, "claude:"):
		return strings.ReplaceAll(strings.TrimPrefix(author, "claude:"), "-", " "), ""
	}
	return author, ""
}

type Store struct{ db *store.DB }

func New(db *store.DB) (*Store, error) {
	if _, err := db.Migrate("goals", Schema); err != nil {
		return nil, err
	}
	return &Store{db}, nil
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

var horizons = map[string]bool{"month": true, "quarter": true, "year": true, "ongoing": true}
var cadences = map[string]bool{"daily": true, "weekly": true, "monthly": true}
var statuses = map[string]bool{"active": true, "paused": true, "done": true}

func (s *Store) Create(g Goal) (Goal, error) {
	if strings.TrimSpace(g.Title) == "" {
		return Goal{}, errors.New("title required")
	}
	if g.Horizon == "" {
		g.Horizon = "ongoing"
	}
	if g.Cadence == "" {
		g.Cadence = "weekly"
	}
	if g.Status == "" {
		g.Status = "active"
	}
	if !horizons[g.Horizon] || !cadences[g.Cadence] || !statuses[g.Status] {
		return Goal{}, errors.New("bad horizon/cadence/status")
	}
	g.ID = slug(g.Title)
	if g.ID == "" {
		return Goal{}, errors.New("title must contain letters")
	}
	now := time.Now().UTC()
	g.CreatedAt, g.UpdatedAt = now, now
	_, err := s.db.Exec(`INSERT INTO goals (id,created_at,updated_at,title,statement,horizon,cadence,status,sources) VALUES (?,?,?,?,?,?,?,?,?)`,
		g.ID, ts(now), ts(now), g.Title, g.Statement, g.Horizon, g.Cadence, g.Status, g.Sources)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Goal{}, fmt.Errorf("goal %q already exists", g.ID)
		}
		return Goal{}, err
	}
	g.Emblem = EmblemFor(g.ID, g.Title)
	return g, nil
}

// Update changes mutable fields (statement, horizon, cadence, status, sources).
func (s *Store) Update(id string, patch map[string]string) (Goal, error) {
	g, err := s.Get(id)
	if err != nil {
		return Goal{}, err
	}
	digest := false
	for k, v := range patch {
		switch k {
		case "title":
			g.Title = v
		case "statement":
			g.Statement = v
		case "horizon":
			if !horizons[v] {
				return Goal{}, errors.New("bad horizon")
			}
			g.Horizon = v
		case "cadence":
			if !cadences[v] {
				return Goal{}, errors.New("bad cadence")
			}
			g.Cadence = v
		case "status":
			if !statuses[v] {
				return Goal{}, errors.New("bad status")
			}
			g.Status = v
		case "sources":
			g.Sources = v
		case "digest":
			g.Digest = v
			digest = true
		default:
			return Goal{}, fmt.Errorf("cannot update %q", k)
		}
	}
	now := time.Now().UTC()
	_, err = s.db.Exec(`UPDATE goals SET title=?,statement=?,horizon=?,cadence=?,status=?,sources=?,updated_at=? WHERE id=?`,
		g.Title, g.Statement, g.Horizon, g.Cadence, g.Status, g.Sources, ts(now), id)
	if err != nil {
		return Goal{}, err
	}
	// digest_at dates the summary, not the goal: a stale digest must be visible
	// as stale, so it is only stamped when the digest itself is rewritten.
	if digest {
		if _, err := s.db.Exec(`UPDATE goals SET digest=?,digest_at=? WHERE id=?`, g.Digest, ts(now), id); err != nil {
			return Goal{}, err
		}
	}
	return s.Get(id)
}

func (s *Store) Get(id string) (Goal, error) {
	return scan(s.db.QueryRow(`SELECT g.id,g.created_at,g.updated_at,g.title,g.statement,g.horizon,g.cadence,g.status,g.sources,g.last_reviewed_at,
		(SELECT COUNT(*) FROM goal_notes n WHERE n.goal_id=g.id),g.digest,g.digest_at FROM goals g WHERE g.id=?`, id))
}

func (s *Store) List(status string) ([]Goal, error) {
	// The list deliberately omits the digest: `lifectl goals` is the first call
	// of every session and must stay small. Digests are read one goal at a time.
	q := `SELECT g.id,g.created_at,g.updated_at,g.title,g.statement,g.horizon,g.cadence,g.status,g.sources,g.last_reviewed_at,
		(SELECT COUNT(*) FROM goal_notes n WHERE n.goal_id=g.id),'',NULL FROM goals g `
	var args []any
	if status != "" {
		q += `WHERE g.status=? `
		args = append(args, status)
	}
	q += `ORDER BY g.status='active' DESC, g.created_at`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Goal{}
	for rows.Next() {
		g, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) AddNote(goalID, author, kind, text string) (Note, error) {
	if _, err := s.Get(goalID); err != nil {
		return Note{}, err
	}
	if strings.TrimSpace(text) == "" {
		return Note{}, errors.New("empty note")
	}
	if kind == "" {
		kind = "note"
	}
	now := time.Now().UTC()
	res, err := s.db.Exec(`INSERT INTO goal_notes (goal_id,created_at,author,kind,text) VALUES (?,?,?,?,?)`, goalID, ts(now), author, kind, text)
	if err != nil {
		return Note{}, err
	}
	id, _ := res.LastInsertId()
	if kind == "review" {
		s.db.Exec(`UPDATE goals SET last_reviewed_at=? WHERE id=?`, ts(now), goalID)
	}
	n := Note{ID: id, GoalID: goalID, CreatedAt: now, Author: author, Kind: kind, Text: text}
	var title string
	s.db.QueryRow(`SELECT title FROM threads WHERE id=?`, strings.TrimPrefix(author, "claude:thread:")).Scan(&title)
	n.By, n.ThreadID = byLine(author, title)
	return n, nil
}

// noteCols is every note query's SELECT list: the note plus the title of the
// session that wrote it. Threads live in the same database but belong to
// another package, so a database without that table (this package's own
// tests) reads an empty title instead of failing.
func (s *Store) noteCols() string {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='threads'`).Scan(&n)
	if n == 0 {
		return `n.id,n.goal_id,n.created_at,n.author,n.kind,n.text,'' FROM goal_notes n`
	}
	return `n.id,n.goal_id,n.created_at,n.author,n.kind,n.text,COALESCE(t.title,'') FROM goal_notes n
	LEFT JOIN threads t ON n.author LIKE 'claude:thread:%' AND t.id=substr(n.author,15)`
}

func (s *Store) Notes(goalID string, limit int) ([]Note, error) {
	return s.query(`SELECT `+s.noteCols()+` WHERE n.goal_id=? ORDER BY n.id DESC LIMIT ?`, goalID, limit)
}

// SinceDigest: the state notes the digest does not know yet, newest first —
// decision/context/intent change what is TRUE; evidence and progress are logs
// (a daily run's "nothing changed" is evidence).
// A session reads the digest as the goal's current state, so every decision
// written after digest_at is a fact it would otherwise miss: a daily run that
// reads only the digest plus its own newest logs misses a decision in between.
func (s *Store) SinceDigest(goalID string, limit int) ([]Note, error) {
	return s.query(`SELECT `+s.noteCols()+` JOIN goals g ON g.id=n.goal_id
		WHERE n.goal_id=? AND n.kind IN ('decision','context','intent') AND n.created_at > COALESCE(g.digest_at,'')
		ORDER BY n.id DESC LIMIT ?`, goalID, limit)
}

func (s *Store) query(q string, args ...any) ([]Note, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		var n Note
		var c, title string
		if err := rows.Scan(&n.ID, &n.GoalID, &c, &n.Author, &n.Kind, &n.Text, &title); err != nil {
			return nil, err
		}
		n.CreatedAt, _ = time.Parse(time.RFC3339Nano, c)
		n.By, n.ThreadID = byLine(n.Author, title)
		out = append(out, n)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scan(r scanner) (Goal, error) {
	var g Goal
	var c, u string
	var lr, da sql.NullString
	if err := r.Scan(&g.ID, &c, &u, &g.Title, &g.Statement, &g.Horizon, &g.Cadence, &g.Status, &g.Sources, &lr, &g.NoteCount, &g.Digest, &da); err != nil {
		return Goal{}, err
	}
	g.CreatedAt, _ = time.Parse(time.RFC3339Nano, c)
	g.UpdatedAt, _ = time.Parse(time.RFC3339Nano, u)
	if lr.Valid {
		t, _ := time.Parse(time.RFC3339Nano, lr.String)
		g.LastReviewedAt = &t
	}
	if da.Valid {
		t, _ := time.Parse(time.RFC3339Nano, da.String)
		g.DigestAt = &t
	}
	g.Emblem = EmblemFor(g.ID, g.Title)
	return g, nil
}

func ts(t time.Time) string { return store.TS(t) }

package calendar

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"life/hub/internal/store"
)

// Away: days the owner cannot be booked (a trip, a holiday), a label over a
// span of days. It is not an item: it fires nothing, nags nothing, raises no
// ask and never counts on a badge. It exists so a dated idea meets it: the
// preamble tells every session to check `lifectl cal` and `lifectl cal away`
// before suggesting a day, and a step or dated ask filed on one of those days
// comes back with `away` saying so. The dates themselves stay out of the
// preamble, which every turn re-reads. Ended, never deleted: `state` on ->
// ended.
type Away struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail,omitempty"`
	From      string    `json:"from"`  // YYYY-MM-DD local, first day away
	Until     string    `json:"until"` // YYYY-MM-DD local, last day away (inclusive)
	Source    string    `json:"source"`
	State     string    `json:"state"` // on | ended
	Note      string    `json:"note,omitempty"`
}

const awayCols = `id, created_at, title, detail, day_from, day_until, source, state, note`

func scanAway(r scanner) (Away, error) {
	var a Away
	var created string
	if err := r.Scan(&a.ID, &created, &a.Title, &a.Detail, &a.From, &a.Until, &a.Source, &a.State, &a.Note); err != nil {
		return a, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return a, nil
}

// AddAway records a span. Until "" = the one day.
func (c *Calendar) AddAway(a Away) (Away, error) {
	a.Title = strings.TrimSpace(a.Title)
	if a.Title == "" {
		return Away{}, errors.New("an away span needs a title (\"Trip\")")
	}
	if a.Until == "" {
		a.Until = a.From
	}
	for _, d := range []string{a.From, a.Until} {
		if _, err := time.ParseInLocation("2006-01-02", d, time.Local); err != nil {
			return Away{}, errors.New("from and until must be YYYY-MM-DD")
		}
	}
	if a.Until < a.From {
		return Away{}, errors.New("until is before from")
	}
	now := c.Now()
	a.ID, a.CreatedAt, a.State, a.Detail = store.NewID("away"), now, "on", strings.TrimSpace(a.Detail)
	if _, err := c.db.Exec(`INSERT INTO away (`+awayCols+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		a.ID, ts(now), a.Title, a.Detail, a.From, a.Until, a.Source, a.State, ""); err != nil {
		return Away{}, err
	}
	log.Printf("calendar: %s away %s..%s: %s", a.ID, a.From, a.Until, a.Title)
	return a, nil
}

// EndAway takes a span off (plans changed); the row stays.
func (c *Calendar) EndAway(id, note string) (Away, error) {
	res, err := c.db.Exec(`UPDATE away SET state='ended', note=? WHERE id=? AND state='on'`, strings.TrimSpace(note), id)
	if err != nil {
		return Away{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Away{}, errors.New("no such away span on")
	}
	return scanAway(c.db.QueryRow(`SELECT `+awayCols+` FROM away WHERE id=?`, id))
}

// Aways: the spans on that end on or after `from` and start on or before
// `to` ("" = no end), soonest first.
func (c *Calendar) Aways(from, to string) ([]Away, error) {
	if to == "" {
		to = "9999-12-31"
	}
	rows, err := c.db.Query(`SELECT `+awayCols+` FROM away WHERE state='on' AND day_until >= ? AND day_from <= ? ORDER BY day_from, id`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Away{}
	for rows.Next() {
		a, err := scanAway(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AwayOn: what a step or dated ask filed on `day` is told ("" = the owner is
// free that day, or no day).
func (c *Calendar) AwayOn(day string) string {
	if day == "" {
		return ""
	}
	list, err := c.Aways(day, day)
	if err != nil || len(list) == 0 {
		return ""
	}
	var names []string
	for _, a := range list {
		names = append(names, a.Title+" "+span(a))
	}
	return "The owner is away on " + day + ": " + strings.Join(names, "; ") + ". They cannot do or attend anything that day: move it unless it is meant for the trip itself."
}

func span(a Away) string {
	if a.Until == a.From {
		return a.From
	}
	return fmt.Sprintf("%s..%s", a.From, a.Until)
}

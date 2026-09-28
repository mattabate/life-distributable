package store

import "time"

// Log names an object table with a state column and its append-only event
// log (review-primitives step 4, 2026-09-26): recs had no history at all and
// a calendar Reopen nulled the close off the row, so "who closed this, when,
// with what words" was lost the moment it changed. Every state write of such
// a table goes through Move, which is the ONLY writer of its events table.
//
// The events table has the columns ask_events has: (<Key>, ts, actor,
// from_state, to_state, note), plus its integer id.
type Log struct {
	Table, Events, Key, State string
}

// Move runs one state write on one row (an UPDATE with its own guard, or the
// INSERT that creates it) and appends its event, in one transaction. The
// row's state is read before and after inside the transaction, so the event
// says what really happened. A write that touched no row logs nothing and
// reports moved=false; with always=false a write that left the state where
// it was (an edit) logs nothing either. The caller's SQL is kept whole: each
// move still owns its columns and its guard (only a row still `scheduled`
// fires).
func (l Log) Move(db *DB, id, by, note string, always bool, now time.Time, query string, args ...any) (moved bool, err error) {
	return l.MoveWith(db, id, by, note, always, now, Stmt{query, args})
}

// Stmt is one SQL statement and its arguments.
type Stmt struct {
	Q    string
	Args []any
}

// MoveWith is Move with side writes: each of `side` runs in the same
// transaction once the move has touched its row — a rec's ledger fields or an
// action's run record, which live in their own table under the item's id. On
// the items table the row's verb, window and lane are re-derived as well
// (ItemStampSet), so every state write keeps them true.
func (l Log) MoveWith(db *DB, id, by, note string, always bool, now time.Time, move Stmt, side ...Stmt) (moved bool, err error) {
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var from, to string
	tx.QueryRow(`SELECT `+l.State+` FROM `+l.Table+` WHERE id=?`, id).Scan(&from)
	res, err := tx.Exec(move.Q, move.Args...)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	for _, s := range side {
		if _, err := tx.Exec(s.Q, s.Args...); err != nil {
			return false, err
		}
	}
	if l.Table == "items" {
		if err := StampItem(tx, id); err != nil {
			return false, err
		}
	}
	if err := tx.QueryRow(`SELECT `+l.State+` FROM `+l.Table+` WHERE id=?`, id).Scan(&to); err != nil {
		return false, err
	}
	if always || from != to {
		if _, err := tx.Exec(`INSERT INTO `+l.Events+` (`+l.Key+`, ts, actor, from_state, to_state, note) VALUES (?,?,?,?,?,?)`,
			id, TS(now), by, from, to, note); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

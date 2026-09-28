package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

// The handler helpers (review 2026-09-26 §5): one body decoder with a size
// cap, one integer query parser, one "not configured" answer and one error
// mapper, where there were ~50 copies of each.

// maxJSON is the body cap when a handler names none. The largest JSON a
// handler takes without its own cap is a statement submission (8 MB, capped
// there); nothing else comes close.
const maxJSON = 4 << 20

// decode reads r's JSON body into v, at most max bytes (0 = maxJSON). On a
// bad or oversized body it answers 400 and returns false.
func decode(w http.ResponseWriter, r *http.Request, v any, max int64) bool {
	if max <= 0 {
		max = maxJSON
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, max)).Decode(v); err != nil {
		jsonErr(w, 400, "bad json: "+err.Error())
		return false
	}
	return true
}

// decodeOptional is decode for a body that may be absent: no body leaves v
// as it was; a body that is there must parse.
func decodeOptional(w http.ResponseWriter, r *http.Request, v any, max int64) bool {
	if max <= 0 {
		max = maxJSON
	}
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, max)).Decode(v)
	if err != nil && !errors.Is(err, io.EOF) {
		jsonErr(w, 400, "bad json: "+err.Error())
		return false
	}
	return true
}

// qInt is the integer query parameter name: def when it is missing, not a
// number, not positive, or above max (max 0 = no ceiling).
func qInt(r *http.Request, name string, def, max int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || n <= 0 || (max > 0 && n > max) {
		return def
	}
	return n
}

// need answers 503 with msg when a store or connector is not wired (ok is
// false) and returns ok.
func need(w http.ResponseWriter, ok bool, msg string) bool {
	if !ok {
		jsonErr(w, 503, msg)
	}
	return ok
}

// fail answers an error from a store: a row that is not there is 404 with
// missing ("no such person"), anything else 500.
func fail(w http.ResponseWriter, err error, missing string) {
	if errors.Is(err, sql.ErrNoRows) {
		jsonErr(w, 404, missing)
		return
	}
	jsonErr(w, 500, err.Error())
}

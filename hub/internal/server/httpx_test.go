package server

import (
	"database/sql"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerHelpers(t *testing.T) {
	var v struct{ A int }

	w := httptest.NewRecorder()
	if !decode(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"a":3}`)), &v, 0) || v.A != 3 {
		t.Fatalf("decode: %v %d", v, w.Code)
	}
	w = httptest.NewRecorder()
	if decode(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"a":`+strings.Repeat("1", 64)+`}`)), &v, 16) || w.Code != 400 {
		t.Fatalf("an oversized body should be a 400, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	if decode(w, httptest.NewRequest("POST", "/", nil), &v, 0) || w.Code != 400 {
		t.Fatalf("a missing body should be a 400 for decode, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	if !decodeOptional(w, httptest.NewRequest("POST", "/", nil), &v, 0) {
		t.Fatalf("a missing body is fine for decodeOptional, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	if decodeOptional(w, httptest.NewRequest("POST", "/", strings.NewReader(`{`)), &v, 0) || w.Code != 400 {
		t.Fatalf("a malformed optional body should be a 400, got %d", w.Code)
	}

	for q, want := range map[string]int{"": 50, "?limit=x": 50, "?limit=0": 50, "?limit=-3": 50, "?limit=7": 7, "?limit=501": 50, "?limit=500": 500} {
		if got := qInt(httptest.NewRequest("GET", "/"+q, nil), "limit", 50, 500); got != want {
			t.Errorf("qInt %q = %d, want %d", q, got, want)
		}
	}

	w = httptest.NewRecorder()
	if need(w, false, "art not configured") || w.Code != 503 || !strings.Contains(w.Body.String(), "art not configured") {
		t.Fatalf("need: %d %s", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	fail(w, sql.ErrNoRows, "no such person")
	if w.Code != 404 || !strings.Contains(w.Body.String(), "no such person") {
		t.Fatalf("fail no rows: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	fail(w, errors.New("disk"), "no such person")
	if w.Code != 500 {
		t.Fatalf("fail other: %d", w.Code)
	}
}

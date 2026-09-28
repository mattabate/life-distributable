package server

import (
	"net/http"
	"testing"
	"time"
)

func TestLogRequestSkipsFastGoodReads(t *testing.T) {
	for _, c := range []struct {
		method, path string
		code         int
		took         time.Duration
		want         bool
	}{
		{"GET", "/api/v1/board", 200, 40 * time.Millisecond, false},
		{"GET", "/api/v1/board", 304, 10 * time.Millisecond, false},
		{"GET", "/api/v1/board", 200, 2500 * time.Millisecond, true},
		{"GET", "/api/v1/board", 500, time.Millisecond, true},
		{"GET", "/api/v1/threads/x", 404, time.Millisecond, true},
		{"POST", "/api/v1/prompts", 201, time.Millisecond, true},
		{"GET", "/api/v1/changes", 200, 25 * time.Second, false},
	} {
		if got := logRequest(c.method, c.path, c.code, c.took); got != c.want {
			t.Errorf("%s %s %d %s: got %v", c.method, c.path, c.code, c.took, got)
		}
	}
	var _ http.Flusher = &statusWriter{}
	var _ http.Hijacker = &statusWriter{}
}

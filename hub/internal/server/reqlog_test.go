package server

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

// The request log never prints a path credential (the OTA token).
func TestLogPathRedactsPathSecrets(t *testing.T) {
	for in, want := range map[string]string{
		"/ota/0123abcd/Life.ipa":       "/ota/REDACTED/Life.ipa",
		"/ota/0123abcd/install.html":   "/ota/REDACTED/install.html",
		"/ota/":                        "/ota/",
		"/api/v1/board":                "/api/v1/board",
		"/api/v1/threads/thr-1/events": "/api/v1/threads/thr-1/events",
	} {
		if got := logPath(in); got != want {
			t.Errorf("logPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// End to end: an OTA fetch through ServeHTTP leaves no token in the log.
func TestOTARequestLogHasNoToken(t *testing.T) {
	s := newTest(t)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/ota/sekrit-token-123/Life.ipa", nil))
	if strings.Contains(buf.String(), "sekrit-token-123") {
		t.Fatalf("token in the request log: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "/ota/REDACTED/Life.ipa") {
		t.Fatalf("request not logged as redacted: %q", buf.String())
	}
}

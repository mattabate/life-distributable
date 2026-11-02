package server

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A JSON body past maxJSON is refused, not read.
func TestOversizedJSONBodyIsRefused(t *testing.T) {
	s := newTest(t)
	big := s.do(t, "POST", "/api/v1/asks", map[string]string{"title": strings.Repeat("x", maxJSON+1)})
	if big.Code != 400 || !strings.Contains(big.Body.String(), "bad json") {
		t.Fatalf("oversized body: %d %s, want 400 bad json", big.Code, big.Body.String())
	}
	small := s.do(t, "POST", "/api/v1/asks", map[string]string{"title": "x"})
	if strings.Contains(small.Body.String(), "bad json") {
		t.Fatalf("a small body was refused as bad json: %d %s", small.Code, small.Body.String())
	}
}

// No handler decodes or slurps r.Body without a cap.
func TestNoUncappedBody(t *testing.T) {
	bad := regexp.MustCompile(`json\.NewDecoder\(r\.Body\)|io\.ReadAll\(r\.Body\)|io\.Copy\([^)]*, r\.Body\)`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if bad.MatchString(line) {
				t.Errorf("%s:%d reads r.Body with no cap; use jsonBody/decode or http.MaxBytesReader", f, i+1)
			}
		}
	}
}

// A body that stops arriving is cut after bodyIdle; a body read to its end
// takes the deadline off, so the handler may then run past it (a land, a
// parked poll) without its request being cancelled.
func TestBodyDeadline(t *testing.T) {
	old := bodyIdle
	bodyIdle = 200 * time.Millisecond
	t.Cleanup(func() { bodyIdle = old })

	got := make(chan error, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timeBody(w, r)
		_, err := io.ReadAll(r.Body)
		if err == nil {
			time.Sleep(4 * bodyIdle)
			err = r.Context().Err()
		}
		got <- err
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Stalled: says 100 bytes, sends 5.
	c, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	io.WriteString(c, "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\nhello")
	select {
	case err := <-got:
		if err == nil {
			t.Fatal("stalled body read without error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stalled body was not cut off")
	}

	// Complete: the handler outlives bodyIdle and its context stays live.
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := <-got; err != nil {
		t.Fatalf("handler after a complete body: %v, want a live request", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("complete body: %d", resp.StatusCode)
	}
}

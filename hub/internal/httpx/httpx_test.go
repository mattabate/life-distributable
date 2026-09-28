package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != "k" {
			http.Error(w, strings.Repeat("no key ", 100), http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"n":3}`))
	}))
	defer srv.Close()
	var out struct{ N int }
	if err := JSON(context.Background(), nil, "GET", srv.URL, map[string]string{"X-Key": "k"}, nil, &out); err != nil || out.N != 3 {
		t.Fatal(out, err)
	}
	err := JSON(context.Background(), srv.Client(), "GET", srv.URL, nil, nil, &out)
	if err == nil || !strings.HasPrefix(err.Error(), "HTTP 403: no key") || len(err.Error()) > 320 {
		t.Fatalf("err = %v", err)
	}
}

func TestRetry429(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`ok`))
	}))
	defer srv.Close()
	get := func() (*http.Response, error) { return http.Get(srv.URL) }
	resp, err := retry429(context.Background(), 3, time.Millisecond, get)
	if err != nil || resp.StatusCode != 200 || n != 3 {
		t.Fatal(resp, err, n)
	}
	resp.Body.Close()
	n = 0
	if _, err := retry429(context.Background(), 1, time.Millisecond, get); !errors.Is(err, ErrRateLimited) || n != 2 {
		t.Fatalf("err = %v after %d calls", err, n)
	}
}

func TestBytesStatusErrorAndPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "":
			w.Write([]byte("2"))
		case "2":
			// last page: no next token
		case "gone":
			http.Error(w, "no such thing", http.StatusNotFound)
		default:
			w.Write([]byte(strings.Repeat("x", 100)))
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	_, err := Do(ctx, nil, "GET", srv.URL+"?page=gone", nil, nil, 0)
	if Code(err) != 404 || err.Error() != "HTTP 404: no such thing" {
		t.Fatalf("err = %v", err)
	}
	var se *StatusError
	if !errors.As(err, &se) || se.Status() != "404 Not Found" {
		t.Fatalf("status = %v", err)
	}
	if b, err := Do(ctx, nil, "GET", srv.URL+"?page=big", nil, nil, 10); err != nil || len(b) != 10 {
		t.Fatalf("cap: %d %v", len(b), err)
	}
	var seen []string
	err = Pages(ctx, 0, func(ctx context.Context, tok string) (string, error) {
		seen = append(seen, tok)
		b, err := Do(ctx, nil, "GET", srv.URL+"?page="+tok, nil, nil, 0)
		return string(b), err
	})
	if err != nil || strings.Join(seen, ",") != ",2" {
		t.Fatalf("pages %q %v", seen, err)
	}
	// A server that never stops handing out a token stops at the cap.
	n := 0
	Pages(ctx, 3, func(ctx context.Context, tok string) (string, error) { n++; return tok + "x", nil })
	if n != 3 {
		t.Fatalf("capped pager ran %d pages", n)
	}
}

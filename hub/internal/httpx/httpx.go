// Package httpx is the connectors' shared HTTP plumbing: one JSON call with a
// bounded body and a readable error, one 429 backoff loop, and the nil-client
// default, so a connector never grows its own copy.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MaxBody caps what a JSON call reads from one response.
const MaxBody = 8 << 20

// ErrRateLimited is what Retry429 returns once its retries are spent.
var ErrRateLimited = errors.New("rate limited; will retry")

// Default is the client a nil one means. Unlike http.DefaultClient it has a
// timeout: a connector whose server stops answering must not hold its clock
// task (and so every later run of it) forever.
var Default = &http.Client{Timeout: 2 * time.Minute}

// Or is c, or Default when c is nil.
func Or(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return Default
}

// JSON sends one request (body, when non-nil, is marshalled as JSON), and
// decodes a 2xx response into out. Any other status is an error carrying the
// code and the first 300 bytes of the body.
func JSON(ctx context.Context, c *http.Client, method, u string, hdr map[string]string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	b, err := Bytes(c, req, MaxBody)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// StatusError is a non-2xx answer: its code and the start of what it said.
// Callers that branch on the code use errors.As; everyone else prints it.
type StatusError struct {
	Code int
	Body string // trimmed, at most 300 bytes
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("HTTP %d", e.Code)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Code, e.Body)
}

// Status is the code with its standard text, "403 Forbidden" — what
// resp.Status read, for errors that never quoted the body.
func (e *StatusError) Status() string {
	return fmt.Sprintf("%d %s", e.Code, http.StatusText(e.Code))
}

// Code is the HTTP status behind err, or 0 when err is not a StatusError.
func Code(err error) int {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code
	}
	return 0
}

// Bytes sends req and returns the body of a 2xx answer, read up to max bytes
// (0 = MaxBody). Any other status is a *StatusError. The one copy of the
// "read the body, check the status, keep a snippet" block every connector
// used to carry (review 2026-09-26 §2).
func Bytes(c *http.Client, req *http.Request, max int64) ([]byte, error) {
	if max <= 0 {
		max = MaxBody
	}
	resp, err := Or(c).Do(req)
	if err != nil {
		return nil, err
	}
	return Read(resp, max)
}

// Read is Bytes for a response the caller already has (after Retry429, say):
// it closes the body.
func Read(resp *http.Response, max int64) ([]byte, error) {
	defer resp.Body.Close()
	if max <= 0 {
		max = MaxBody
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max))
	if resp.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(b))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, &StatusError{Code: resp.StatusCode, Body: msg}
	}
	return b, err
}

// Do builds one request (hdr set verbatim, body sent as is) and returns what
// Bytes returns.
func Do(ctx context.Context, c *http.Client, method, u string, hdr map[string]string, body io.Reader, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return Bytes(c, req, max)
}

// Pages walks a paged API: fetch reads one page by its token ("" = the
// first) and returns the next token, "" when it was the last. It stops after
// max pages (0 = no cap), so a server that keeps handing back a token cannot
// hold a sync forever.
func Pages(ctx context.Context, max int, fetch func(ctx context.Context, token string) (next string, err error)) error {
	token := ""
	for n := 0; max == 0 || n < max; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		next, err := fetch(ctx, token)
		if err != nil {
			return err
		}
		if next == "" || next == token {
			return nil
		}
		token = next
	}
	return nil
}

// Retry429 calls do until it answers something other than 429, waiting 2s,
// 4s, 8s… between tries; after `retries` waits it gives up with
// ErrRateLimited. A burst limit is a pause, not a verdict. The caller owns the
// returned response's body.
func Retry429(ctx context.Context, retries int, do func() (*http.Response, error)) (*http.Response, error) {
	return retry429(ctx, retries, 2*time.Second, do)
}

func retry429(ctx context.Context, retries int, backoff time.Duration, do func() (*http.Response, error)) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := do()
		if err != nil || resp.StatusCode != http.StatusTooManyRequests {
			return resp, err
		}
		resp.Body.Close()
		if attempt >= retries {
			return nil, ErrRateLimited
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

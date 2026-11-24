package spend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Claude Code on Linux keeps its login in ~/.claude/.credentials.json, the
// Keychain item's JSON; the hub reads the token from there, and no error
// ever carries the token.
func TestCredentialsFileToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, ".credentials.json")
	if credentialsFile() != path {
		t.Fatalf("credentialsFile = %q, want %q", credentialsFile(), path)
	}
	if _, err := fileToken(path); err == nil {
		t.Fatal("a missing file gave a token")
	}
	later := time.Now().Add(time.Hour).UnixMilli()
	write := func(s string) {
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"claudeAiOauth":{"accessToken":"sk-fake-1","expiresAt":` + strconv.FormatInt(later, 10) + `}}`)
	if tok, err := fileToken(path); err != nil || tok != "sk-fake-1" {
		t.Fatalf("fileToken = %q, %v", tok, err)
	}
	write(`{"claudeAiOauth":{"accessToken":"sk-fake-2","expiresAt":1}}`)
	if _, err := fileToken(path); err == nil || strings.Contains(err.Error(), "sk-fake-2") {
		t.Fatalf("expired token: err = %v", err)
	}
	write(`{"claudeAiOauth":{"accessToken":"sk-fake-3"`)
	if _, err := fileToken(path); err == nil || strings.Contains(err.Error(), "sk-fake-3") {
		t.Fatalf("broken JSON: err = %v", err)
	}
	write(`{}`)
	if _, err := fileToken(path); !errors.Is(err, ErrAPILogin) {
		t.Fatalf("no claudeAiOauth: err = %v, want ErrAPILogin", err)
	}
}

// An org's managed-key login has no claude.ai plan (#262): the meter says
// "billed by the API" in grey, never a red error, and the picker has no
// windows to steer by.
func TestAnAPILoginIsNotAnError(t *testing.T) {
	q := &QuotaFetcher{Token: func(context.Context) (string, error) { return "", ErrAPILogin }}
	got := q.Quota(context.Background(), nil, time.Now())
	if got.Available || got.Error != "" || got.Billing != "api" {
		t.Fatalf("quota = available %v, error %q, billing %q", got.Available, got.Error, got.Billing)
	}
}

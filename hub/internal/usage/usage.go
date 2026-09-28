// Package usage is the opt-in weekly heartbeat: the only call the hub makes
// on its own to anywhere but the owner's devices. It says an install is alive
// and roughly how much it is used — a random install id, the version, and
// counts. Never text, names, amounts, paths or the host.
//
// Off unless the owner said yes at setup (`usage_opt_in` in ops/hub.json);
// after that the switch is the `usage:on` setting, which Settings flips
// (PUT /api/v1/usage). Turning it off stops the next send; nothing is queued.
package usage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"life/hub/internal/spend"
	"life/hub/internal/store"
)

// Version is the distributable release the heartbeat reports.
const Version = "0.1.0"

// Defaults: PostHog US Cloud and the project's public, write-only token
// (safe to ship: it can send events, never read them).
const (
	DefaultHost  = "https://us.i.posthog.com"
	DefaultToken = "phc_qRvXx8ciuvFtsSqF2C4widbHcKrQsu7YeWi9PmAg7xXc"
)

// Heartbeat reads its counts from the hub's own tables.
type Heartbeat struct {
	DB     *store.DB
	Host   string
	Token  string
	Client *http.Client
	// Usages is the transcript cache (spend.Cache.Usages); nil = no token bucket.
	Usages func() ([]spend.Usage, error)
	// Pages is how many tabs the console has (grows when a goal gets a page).
	Pages func() int
	Now   func() time.Time
}

// Seed records the setup answer once; later the setting is the switch.
func (h *Heartbeat) Seed(optIn bool) {
	if h.DB.Setting("usage:on") == "" {
		h.SetOn(optIn)
	}
}

// On: the owner has the heartbeat switched on.
func (h *Heartbeat) On() bool { return h.DB.Setting("usage:on") == "yes" }

// SetOn flips the switch.
func (h *Heartbeat) SetOn(on bool) error {
	v := "no"
	if on {
		v = "yes"
	}
	return h.DB.SetSetting("usage:on", v)
}

// InstallID is random, made on first use, and stays with the database.
func (h *Heartbeat) InstallID() string {
	if id := h.DB.Setting("usage:install_id"); id != "" {
		return id
	}
	b := make([]byte, 16)
	rand.Read(b)
	id := hex.EncodeToString(b)
	h.DB.SetSetting("usage:install_id", id)
	return id
}

func (h *Heartbeat) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Payload is exactly what a send carries (Settings shows it).
func (h *Heartbeat) Payload() map[string]any {
	since := store.TS(h.now().AddDate(0, 0, -7))
	count := func(q string, args ...any) int {
		var n int
		h.DB.QueryRow(q, args...).Scan(&n)
		return n
	}
	p := map[string]any{
		"version":          Version,
		"active_days":      count(`SELECT COUNT(DISTINCT substr(ts,1,10)) FROM thread_messages WHERE role='owner' AND ts>=?`, since),
		"sessions_started": count(`SELECT COUNT(*) FROM threads WHERE created_at>=?`, since),
		"goals":            count(`SELECT COUNT(*) FROM goals WHERE status='active'`),
		"token_bucket":     "unknown",
	}
	if h.Pages != nil {
		p["pages"] = h.Pages()
	}
	if h.Usages != nil {
		if us, err := h.Usages(); err == nil {
			cut := h.now().AddDate(0, 0, -7)
			n := 0
			for _, u := range us {
				if u.TS.After(cut) {
					n += u.Input + u.Output + u.CacheRead + u.CacheWrite5m + u.CacheWrite1h
				}
			}
			p["token_bucket"] = Bucket(n)
		}
	}
	return p
}

// Bucket rounds a week's tokens to an order of magnitude.
func Bucket(n int) string {
	switch {
	case n == 0:
		return "0"
	case n < 1_000_000:
		return "<1M"
	case n < 10_000_000:
		return "1-10M"
	case n < 100_000_000:
		return "10-100M"
	case n < 1_000_000_000:
		return "100M-1B"
	}
	return "1B+"
}

// Send posts one heartbeat when the switch is on; off is a quiet no-op.
func (h *Heartbeat) Send(ctx context.Context) (bool, error) {
	if !h.On() {
		return false, nil
	}
	host, token := h.Host, h.Token
	if host == "" {
		host = DefaultHost
	}
	if token == "" {
		token = DefaultToken
	}
	body, _ := json.Marshal(map[string]any{
		"api_key":     token,
		"event":       "heartbeat",
		"distinct_id": h.InstallID(),
		"properties":  h.Payload(),
	})
	req, err := http.NewRequestWithContext(ctx, "POST", host+"/capture/", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	c := h.Client
	if c == nil {
		c = &http.Client{Timeout: 20 * time.Second}
	}
	res, err := c.Do(req)
	if err != nil {
		return false, err
	}
	res.Body.Close()
	if res.StatusCode/100 != 2 {
		return false, fmt.Errorf("usage: %s", res.Status)
	}
	h.DB.SetSetting("usage:last_sent", store.TS(h.now()))
	return true, nil
}

// LastSent is when the last heartbeat went out ("" = never).
func (h *Heartbeat) LastSent() string { return h.DB.Setting("usage:last_sent") }

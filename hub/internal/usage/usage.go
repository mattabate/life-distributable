// Package usage is the opt-in weekly heartbeat: the only call the hub makes
// on its own to anywhere but the owner's devices. It says an install is alive
// and roughly how much it is used — a random install id, the version, and
// counts. Never text, names, amounts, paths or the host. It goes to the
// project's PostHog unless `usage_host` names another (Settings shows which).
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
const Version = "0.2.0"

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

// Payload is exactly what a send carries (Settings shows it). A count that
// cannot be read is an error, never a silent 0: a heartbeat that understates
// an install is worse than none.
func (h *Heartbeat) Payload() (map[string]any, error) {
	since := store.TS(h.now().AddDate(0, 0, -7))
	var firstErr error
	count := func(q string, args ...any) int {
		var n int
		if err := h.DB.QueryRow(q, args...).Scan(&n); err != nil && firstErr == nil {
			firstErr = err
		}
		return n
	}
	p := map[string]any{
		"version":          Version,
		"sessions_started": count(`SELECT COUNT(*) FROM threads WHERE created_at>=?`, since),
		"goals":            count(`SELECT COUNT(*) FROM goals WHERE status='active'`),
		"token_bucket":     "unknown",
	}
	if days, err := h.activeDays(); err != nil {
		if firstErr == nil {
			firstErr = err
		}
	} else {
		p["active_days"] = days
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
	return p, firstErr
}

// activeDays counts the distinct days with an owner message in the seven
// days ending today, on the hub's own wall clock (store.Eastern, #55). The
// old query counted DISTINCT substr(ts,1,10), which are UTC dates, inside a
// rolling 7x24h window: the window touches eight UTC dates, so it could
// report 8, and an evening message, already tomorrow in UTC, landed on the
// wrong day (#120).
func (h *Heartbeat) activeDays() (int, error) {
	zone := store.Eastern
	now := h.now().In(zone)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone).AddDate(0, 0, -6)
	rows, err := h.DB.Query(`SELECT ts FROM thread_messages WHERE role='owner' AND ts>=?`, store.TS(start))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	days := map[string]bool{}
	for rows.Next() {
		var ts string
		if err := rows.Scan(&ts); err != nil {
			return 0, err
		}
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			continue // an unreadable timestamp is not a day that can be counted
		}
		days[t.In(zone).Format("2006-01-02")] = true
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return len(days), nil
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

// Destination is where a send goes: `usage_host` in ops/hub.json, else the
// project's PostHog. Settings shows it beside the payload.
func (h *Heartbeat) Destination() string {
	if h.Host != "" {
		return h.Host
	}
	return DefaultHost
}

// Send posts one heartbeat when the switch is on; off is a quiet no-op.
func (h *Heartbeat) Send(ctx context.Context) (bool, error) {
	if !h.On() {
		return false, nil
	}
	host, token := h.Destination(), h.Token
	if token == "" {
		token = DefaultToken
	}
	// PostHog would otherwise turn the connection's IP into a city and keep
	// both. `$geoip_disable` stops the lookup; the project's "Discard client
	// IP data" setting (the maintainer's side) drops the IP itself.
	props, err := h.Payload()
	if err != nil {
		return false, err
	}
	props["$geoip_disable"] = true
	body, _ := json.Marshal(map[string]any{
		"api_key":     token,
		"event":       "heartbeat",
		"distinct_id": h.InstallID(),
		"properties":  props,
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

package threads

import "testing"

// The web/mobile separation has to happen without any agent remembering a
// flag, so the inference is the real contract: laptop cards (a console URL,
// an OAuth key) must land on `web`, and the ones the owner acts on from the
// phone must not.
func TestInferSurface(t *testing.T) {
	cases := []struct {
		want, kind, title, detail string
	}{
		{"mobile", "physical", "Install app build 296 (tap the link)", "itms-services://?action=download-manifest&url=https://my-mac.example.ts.net:8443/ota/x/manifest.plist"},
		{"web", "read", "Console Money page rebuilt — open it on the Mac", "https://my-mac.example.ts.net:8443/#/money"},
		{"web", "access", "Send the OAuth 2.0 Client ID", "Register the callback, then paste it: https://developer.example.com/portal"},
		{"web", "access", "Install the Semantic Scholar API key", "Put it in the console's key form."},
		{"any", "decision", "Which batch size for the rollout?", "3 batches of 25, or one of 75 next week?"},
		{"any", "read", "Backups cost 12 a month", "The storage bill is flat over the last 5 months."},
		{"any", "physical", "Connect Apple Health", "Settings › Apple Health › Connect. Grants 17 daily metrics."},
		// A dead run: Restart is one tap and works from either surface, so it
		// stays on both — the phone just draws it as an error cell, not a card.
		{"any", "error", "Session stopped: Claude API 529 (overloaded)", "API Error: 529 Overloaded. If it persists, check https://status.claude.com."},
	}
	for _, c := range cases {
		if got := InferSurface(c.kind, c.title, c.detail); got != c.want {
			t.Errorf("InferSurface(%q, %q) = %q, want %q", c.kind, c.title, got, c.want)
		}
	}
}

// Explicit beats inferred, aliases normalise, and a bad value falls back to
// inference rather than failing an ask.
func TestAskSurface(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Wire the console.", "", "", nil)

	a, err := m.AddAskOn(th.ID, "", "Paste the PostHog key", "In the console.", "access", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Surface != "web" {
		t.Fatalf("inferred %q", a.Surface)
	}
	// Explicit: this read is a phone thing even though it names the console.
	b, _ := m.AddAskOn(th.ID, "", "Recs list is live in the console", "", "read", "", "phone")
	if b.Surface != "mobile" {
		t.Fatalf("explicit %q", b.Surface)
	}
	// Nonsense re-infers ("Paste the … key" → web), never errors.
	c, _ := m.AddAskOn(th.ID, "", "Add the Semantic Scholar API key", "", "other", "", "watch")
	if c.Surface != "web" {
		t.Fatalf("fallback %q", c.Surface)
	}
	// Retag, the escape hatch behind `lifectl ask <id> surface any`.
	d, err := m.SetAskSurface(a.ID, "any")
	if err != nil || d.Surface != "any" {
		t.Fatalf("%+v %v", d, err)
	}
	// Old rows say nothing; the boot backfill classifies the active ones.
	m.db.Exec(`UPDATE items SET surface='any' WHERE id=?`, a.ID)
	m.backfillAskSurface()
	if a, _ = m.GetAsk(a.ID); a.Surface != "web" {
		t.Fatalf("backfill %q", a.Surface)
	}
}

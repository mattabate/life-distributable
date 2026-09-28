// hub: the life hub backend — one binary, bound to the tailnet only.
package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"life/hub/internal/actions"
	"life/hub/internal/budget"
	"life/hub/internal/calendar"
	"life/hub/internal/clock"
	"life/hub/internal/config"
	"life/hub/internal/deliver"
	"life/hub/internal/goals"
	"life/hub/internal/notify"
	"life/hub/internal/obs"
	"life/hub/internal/recs"
	"life/hub/internal/sched"
	"life/hub/internal/server"
	"life/hub/internal/sessions"
	"life/hub/internal/spend"
	"life/hub/internal/store"
	"life/hub/internal/syncruns"
	"life/hub/internal/threads"
	heartbeat "life/hub/internal/usage"
)

func main() {
	store.UseEastern() // every "today" in the hub is Eastern, whatever the Mac says
	home, _ := os.UserHomeDir()
	cfgPath := flag.String("config", filepath.Join(home, "life", "ops", "hub.json"), "config file")
	flag.Parse()
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	token, err := loadOrCreateToken(cfg.TokenFile)
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	// One connection means one careless nested query wedges the hub forever
	// (a rebuild that writes inside its own row loop takes a SIGQUIT to end).
	// The watchdog proves the pool still hands out a
	// connection; if it stops, it dumps the goroutine that holds it and exits
	// so launchd restarts — a permanent silent outage becomes a short, and
	// explained, one.
	store.NewWatchdog(db, filepath.Join(filepath.Dir(cfg.DBPath), "..", "ops", "logs")).Start(context.Background())
	// Model ladder: fable until usage passes its step-down mark (85%), then
	// opus (95%), then sonnet — sessions never stall, and the top models stop
	// being picked well before a plan limit. The policy (ops/hub.json
	// model_policy) says which rung each kind of wake STARTS on; the ladder
	// only ever steps down from there.
	quota := spend.NewQuotaFetcher()
	picker := spend.NewPicker(quota, cfg.ModelLadder, cfg.ModelStepDown)
	// ONE cache of the transcripts for the picker and the server, kept on disk
	// across restarts: a cold walk of the tree was 5–34 s, twice over when
	// each held its own, and every session that ships hub code restarts it
	// (dozens of times a day) — + New session sat blank on it.
	usage := spend.NewCache(cfg.ClaudeProjectsDir)
	usage.Store = filepath.Join(filepath.Dir(cfg.DBPath), "cache", "spend-usages.gob")
	picker.Usages = usage.Usages // the weekly pace line
	policy := cfg.ModelPolicy.Merge(spend.DefaultPolicy())
	// The fable day cap: past its allowance of fable in one
	// Eastern day, every new process starts one rung down (opus) instead —
	// the work still runs, and tomorrow starts at fable again. `guard` is
	// built below (it needs the DB); the closure reads it at launch time.
	var guard *budget.Guard
	pickFrom := func(start string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return picker.PickFrom(ctx, picker.NotAbove(start, guard.Floor()))
	}
	// One-shot runs (jobs, prompts, approved actions) get the rule's flags —
	// model, effort, dollar cap, never a turn count; the model is
	// the rule's rung or the first open one below.
	oneShotArgs := func(trigger string) func() []string {
		rule := policy.Resolve(trigger, "", "")
		return func() []string {
			r := rule
			r.Model = pickFrom(r.Model)
			return r.Args(true)
		}
	}
	exec := actions.DefaultExecutor{ClaudeBin: cfg.ClaudeBin, ProjectDir: func(n string) (string, bool) {
		p, ok := cfg.Project(n)
		return p.Dir, ok
	}, ClaudeArgs: oneShotArgs("job")}
	projDir := func(n string) (string, bool) {
		p, ok := cfg.Project(n)
		return p.Dir, ok
	}
	// The scheduler is the notifier for the actions queue and the threads:
	// NEEDS YOU is a push to the phone, the hub's only notification.
	var acts *actions.Queue
	sch, err := sched.New(db, cfg.SchedulePath, nil, cfg.ClaudeBin, projDir)
	if err != nil {
		log.Fatal(err)
	}
	sch.ModelArgs = oneShotArgs("job")
	var push *notify.APNs
	if cfg.APNsKeyFile != "" && cfg.APNsBundleID != "" {
		push, err = notify.NewAPNs(db, notify.APNsConfig{KeyFile: cfg.APNsKeyFile, KeyID: cfg.APNsKeyID, TeamID: cfg.APNsTeamID, BundleID: cfg.APNsBundleID, Production: cfg.APNsProduction})
		if err != nil {
			log.Fatal(err)
		}
		sch.Push = push
		log.Printf("apns: enabled (production=%v)", cfg.APNsProduction)
	}
	acts = actions.New(db, exec, sch)
	sch.SetActions(acts)
	gs, err := goals.New(db)
	if err != nil {
		log.Fatal(err)
	}
	ob, err := obs.New(db, cfg.BlobDir)
	if err != nil {
		log.Fatal(err)
	}
	sessMgr := sessions.New(cfg.TmuxPrefix, cfg.JobsDir, cfg.ClaudeBin)
	sessMgr.AllowedTools = sch.Table().PromptTools
	sessMgr.ModelArgs = oneShotArgs("prompt")
	thr, err := threads.New(db, cfg.ClaudeBin, filepath.Join(cfg.JobsDir, "threads"), cfg.TmuxPrefix, projDir, sch.Table().PromptTools, sch)
	if err != nil {
		log.Fatal(err)
	}
	thr.OwnerName = cfg.OwnerName
	thr.BlobPath = ob.BlobPath
	thr.AutoCompactWindow = cfg.AutoCompactWindow
	if push != nil {
		// A spoken card marks its session "speaking" on the Sessions list.
		push.Voice = thr.Voice
	}
	quota.Record, quota.Past = quotaHistory(ob)
	// One clock: every housekeeping loop the hub runs is a task on
	// this ticker, so /status can say when each last ran. Started at the end
	// of main, once everything it drives exists.
	ck := clock.New()
	ck.Last = func(name string) time.Time {
		t, _ := time.Parse(time.RFC3339Nano, db.Setting("clock:"+name))
		return t
	}
	ck.Ran = func(name string, at time.Time) { db.SetSetting("clock:"+name, at.UTC().Format(time.RFC3339Nano)) }
	// Every syncer (ck.Sync) run lands in one log, written only here; Sources
	// health reads a connector's last run and failing streak from it.
	runLog, err := syncruns.New(db)
	if err != nil {
		log.Fatal(err)
	}
	ck.Done = func(name string, start, end time.Time, err error) {
		if lerr := runLog.Record(name, start, end, err); lerr != nil {
			log.Printf("sync_runs: %s: %v", name, lerr)
		}
	}
	// Every plan reading is kept, so "did the bar actually move?" is a query
	// and not a guess. Ticking independently means the trace stays continuous
	// while the app is closed.
	ck.Every("quota", 10*time.Minute, true, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		quota.Quota(ctx, nil, time.Now()) // nil usage: we only store the percentages
		return nil
	})
	thr.Model = pickFrom
	thr.Policy = policy
	thr.ModelExhausted = picker.MarkExhausted
	// Ask each rung for one word before a turn lands on it: a model id the
	// CLI is too old for closes that rung and raises one card.
	go thr.PreflightModels(picker.Ladder)
	// Guards over the hub's own spend: the fable day cap (degrade to the next
	// rung, never stop — the owner's knob, `fable_daily_usd`) and, when
	// `daily_budget_usd` is not 0, the older points ladder that pauses
	// unattended wakes. The owner's messages always go through either way.
	guard, err = budget.New(db, policy.DailyBudgetUSD, policy.ThreadDailyCapUSD, policy.FableDailyUSD, picker.Below("fable"))
	if err != nil {
		log.Fatal(err)
	}
	guard.Notify = func(s string) { log.Printf("budget: %s", s) }
	thr.Allow = guard.Allow
	sch.Allow = guard.Allow
	// A job's needs_you line becomes an ask on the idle "hub" thread (the
	// hub's own home, like calendar's fallback thread), so it reaches Your
	// turn like anything else an agent needs from the owner.
	// Made at boot so ops scripts can file on it too (`lifectl ask add …
	// --thread hub`: backup.sh does, on a failed night).
	if _, err := thr.Get("hub"); err != nil {
		if _, err := thr.CreateIdle("hub", "Hub", "life"); err != nil {
			log.Printf("hub thread: %v", err)
		}
	}
	sch.Ask = func(job, text string) {
		if _, err := thr.AddAskOn("hub", "", text, "raised by the "+job+" job", "other", "", ""); err != nil {
			log.Printf("sched: ask from %s: %v", job, err)
		}
	}
	thr.BackfillTitles()
	// The owner's approve/deny (+ note) goes back to the session that proposed
	// it. Actions proposed outside a thread (scheduled jobs) get a fresh
	// session on approval so an agent carries them out and the owner can talk
	// to it.
	acts.OnDecided = func(a actions.Action, approved bool, note string) {
		if a.ExecType == "relay" && approved && note == "" {
			// The hub carries a relay out itself: a bare Approve leaves the
			// proposing session nothing to do, so it is not woken to hear it.
			return
		}
		if a.ThreadID != "" {
			if err := thr.Decision(a.ThreadID, approved, a.ID, a.Title, note); err != nil {
				log.Printf("actions: relay decision to thread %s: %v", a.ThreadID, err)
			}
			return
		}
		if !approved {
			return
		}
		proj := a.Project
		if _, ok := projDir(proj); !ok {
			proj = "life"
		}
		id, _, err := deliver.Deliverer{Thr: thr}.ToSession(deliver.Target{Title: a.Title, Project: proj,
			Starter: threads.DecisionPrompt(true, a.ID, a.Title, a.Detail, note)})
		if err != nil {
			log.Printf("actions: start thread for approved %s: %v", a.ID, err)
			return
		}
		if err := acts.SetThread(a.ID, id); err != nil {
			log.Printf("actions: record thread %s on %s: %v", id, a.ID, err)
		}
	}
	sch.Gate = func(gate string, last time.Time) bool {
		switch gate {
		case "asks":
			return thr.VerifierDue(last)
		}
		return true
	}
	// One clock: every wake — a scheduled session's check-in, a
	// job's run, the owner's "reply at a specific time", an agent asking itself
	// to check back — is a prompts row with a not_before, and this is the
	// one tick that fires them. Jobs' cadences are standing rows reconciled
	// from schedule.json; a due one runs through sch.Fire.
	thr.RunJob = sch.Fire
	thr.JobHeld = sch.Held
	sch.SetClock(thr)
	ck.Every("prompts", time.Minute, false, func() error { thr.DuePrompts(time.Now()); return nil })
	// A prompt answering a proposal names it: "[The owner APPROVED … "<title>"]".
	thr.ActionTitle = func(id string) string {
		a, err := acts.Get(id)
		if err != nil {
			return ""
		}
		return a.Title
	}
	thr.ActionExec = func(id string) string {
		a, err := acts.Get(id)
		if err != nil {
			return ""
		}
		return a.ExecType
	}
	// One session hands another a task only on a card the owner approved (relay.go).
	acts.RelayTarget = thr.RelayTarget
	acts.Relay = thr.Relay
	// A decision that arrives ON a prompt (the card arms the composer)
	// is recorded on the row without the relay above — that prompt is it.
	thr.DecideAction = func(id, outcome, note, via string) error {
		if via == "" {
			via = "app"
		}
		return acts.DecideQuiet(id, outcome == "approved", via, note)
	}
	go func() {
		for {
			time.Sleep(3 * time.Second)
			thr.Poll()
		}
	}()
	h := server.New(cfg, token, sessMgr, acts, sch, gs, ob, thr)
	h.Push = push
	h.Runs = runLog
	h.UseBudget(guard)
	if push != nil {
		// One voice at a time: a push waits while a card is heard or replayed.
		push.Floor = func() bool { return thr.Voice.Busy(time.Now()) }
	}
	// Calendar: dated steps / agent runs / reminders; fires + nags every minute.
	cal, err := calendar.New(db, thr)
	if err != nil {
		log.Fatal(err)
	}
	cal.Nfy = sch
	if push != nil {
		// A card whose line was queued when the last hub stopped is spoken
		// by this one, if it still wants the owner (notify/queue.go; otherwise
		// a restart drops a card unspoken). Every card that goes
		// through notify.Card is asked about here: a proposal, an ask, a
		// calendar nag (open while its item is fired).
		push.Open = func(card string) bool {
			if a, err := acts.Get(card); err == nil {
				return a.State == "proposed"
			}
			// An ask the owner has replied to (`answered`: the ball is the
			// agent's) wants no voice either: a line still waiting for its
			// turn is dropped.
			if k, err := thr.GetAsk(card); err == nil {
				return k.State == "open"
			}
			if it, err := cal.Get(card); err == nil {
				return it.State == "fired"
			}
			return false
		}
		go push.Resume()
		// The app icon wears the console nav's red ovals added up; a minute
		// after the total moves, a badge-only push brings the icon along.
		push.Badge = h.BadgeTotal
		ck.Every("badge", time.Minute, false, push.SyncBadge)
	}
	// The agenda carries every dated object, not only cal_items: actions on
	// their decided/created day, deferred recs on their review day (recs join
	// below, once the store exists), and the asks the owner closed at the
	// minute they closed them — the past as a record of what got done.
	cal.Sources = []calendar.DatedSource{acts, thr}
	h.Cal = cal
	// A session that writes "NEEDS YOU YYYY-MM-DD: …" parks the ask on that
	// morning rather than crowding the board four days early.
	thr.DateAsk = func(threadID, title, detail, kind, check, day, at, surface string) error {
		_, err := cal.Add(calendar.Item{Title: title, Detail: detail, Kind: "owner", Day: day, At: at,
			ThreadID: threadID, Source: "claude:thread:" + threadID, AskKind: kind, CheckHint: check, Surface: surface})
		return err
	}
	// Recommendations: what the agent told the owner to do, and whether it
	// worked. Never notifies (that is what asks are for); the sweep only clears
	// out ideas whose act_by has passed and wakes the ones deferred to a day
	// that has come, so the list stays live.
	rc, err := recs.New(db)
	if err != nil {
		log.Fatal(err)
	}
	h.SetRecs(rc) // also frames prompts that answer a rec (threads.RecHeader)
	cal.Sources = append(cal.Sources, rc)
	ck.Every("recs", 6*time.Hour, true, func() error {
		_, err := rc.Expire()
		if _, werr := rc.Wake(); werr != nil && err == nil {
			err = werr
		}
		return err
	})
	ck.Every("calendar", time.Minute, true, func() error { cal.Tick(); return nil })
	// Reopen "Install app build N" asks the owner tapped but that never landed
	// (phone still reports an older build after 10 min).
	if push != nil {
		ck.Every("install", time.Minute, false, func() error {
			if n := thr.ReconcileInstalls(push.MaxBuild()); n > 0 {
				// Tapped, not landed yet: silent push launches the new
				// build in the background so it reports itself.
				return push.Wake(n)
			}
			return nil
		})
	}
	h.UseQuota(quota)   // one fetcher: the Spend tab and the model picker share its cache
	h.UsePicker(picker) // Spend tab shows which rung new sessions get
	h.UseUsage(usage)   // one transcript cache, the picker's, with its on-disk store
	h.WarmSpend()       // parse the transcripts now, not on the first Spend request
	// The opt-in weekly heartbeat: counts only, and a quiet no-op while off.
	h.Usage = &heartbeat.Heartbeat{DB: db, Host: cfg.UsageHost, Token: cfg.UsageToken,
		Usages: usage.Usages, Pages: server.ConsolePages}
	h.Usage.Seed(cfg.UsageOptIn)
	ck.Every("usage", 7*24*time.Hour, false, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := h.Usage.Send(ctx)
		return err
	})
	h.StatusExtra = func() map[string]any {
		out := statusExtra(cfg)
		out["clock"] = ck.Status()
		return out
	}
	ck.Start()
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
	log.Printf("hub listening on https://%s (projects: %d, jobs: %d)", cfg.ListenAddr, len(cfg.AllProjects()), len(sch.Table().Jobs))
	// Graceful stop: launchd's kickstart -k / bootout sends SIGTERM. In-flight
	// requests (a POST mid-write) get a few seconds to finish; a parked
	// /changes long poll answers at once (h.Draining) so the drain is not held
	// to the deadline — the clients reconnect to the new hub.
	srv.RegisterOnShutdown(h.Draining)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		log.Printf("hub: shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			log.Printf("hub: shutdown: %v", err)
			srv.Close()
		}
	}()
	// ListenAndServeTLS re-reads nothing; cert renewal = restart (ops/renew-cert.sh does that).
	if err := srv.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	// ListenAndServe returns as soon as Shutdown starts; wait for the drain.
	<-drained
	log.Printf("hub: stopped")
}

func loadOrCreateToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err == nil && len(b) >= 32 {
		return string(trimNL(b)), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "generated new hub token at %s\n", path)
	return tok, nil
}

// quotaRetention: how long a plan-quota reading is kept. Seven days is the
// longest meter (`seven_day`), so one full window of readings is always there
// to answer "did the bar actually move this week"; the Spend page itself
// reads only the last 8 h (spend.measuredSpan). Three meters × 144 readings a
// day × 7 days ≈ 3,000 rows — a ceiling instead of 430 new rows a day forever.
const quotaRetention = 7 * 24 * time.Hour

// quotaHistory backs spend.QuotaFetcher's Record/Past with the observations
// table (source `spend`, kind `quota`, one row per meter per fetch). The
// uniq_key is meter+minute, so the app hitting the Spend tab and the 10-minute
// ticker cannot double-write the same reading. The same write prunes readings
// past quotaRetention — the ONLY kind that is ever deleted (obs.PruneQuotaHeartbeats).
func quotaHistory(ob *obs.Store) (func(time.Time, []spend.Window), func(time.Time) []spend.Sample) {
	record := func(at time.Time, ws []spend.Window) {
		if n, err := ob.PruneQuotaHeartbeats(at.Add(-quotaRetention)); err != nil {
			log.Printf("quota history: prune: %v", err)
		} else if n > 0 {
			log.Printf("quota history: pruned %d readings older than %s", n, quotaRetention)
		}
		items := make([]obs.Observation, 0, len(ws))
		for _, w := range ws {
			p, err := json.Marshal(spend.Sample{Key: w.Key, TS: at, Percent: w.Utilization, ResetsAt: w.ResetsAt})
			if err != nil {
				continue
			}
			items = append(items, obs.Observation{
				Source: "spend", Kind: "quota", TS: at, Payload: p,
				UniqKey: w.Key + ":" + at.UTC().Truncate(time.Minute).Format(time.RFC3339),
			})
		}
		if _, _, err := ob.InsertBatch(items); err != nil {
			log.Printf("quota history: %v", err)
		}
	}
	past := func(since time.Time) []spend.Sample {
		rows, err := ob.List(obs.Query{Source: "spend", Kind: "quota", Since: since, Limit: 1000})
		if err != nil {
			log.Printf("quota history: %v", err)
			return nil
		}
		out := make([]spend.Sample, 0, len(rows))
		for _, r := range rows {
			var s spend.Sample
			if err := json.Unmarshal(r.Payload, &s); err != nil || s.Key == "" {
				continue
			}
			s.TS = r.TS
			out = append(out, s)
		}
		return out
	}
	return record, past
}

func trimNL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// statusExtra: environment facts the app shows (LAN-lane profile expiry;
// a paid developer team signs 1-year profiles. OTA builds report their own
// expiry via /status `ota.profile_expires`).
func statusExtra(cfg *config.Config) map[string]any {
	out := map[string]any{}
	b, err := os.ReadFile(cfg.OpsPath("logs", "install-phone.log.last"))
	if err == nil {
		if t, err := time.ParseInLocation("2006-01-02", string(trimNL(b)), time.Local); err == nil {
			exp := t.AddDate(1, 0, 0)
			out["app_installed"] = t.Format("2006-01-02")
			out["app_profile_expires"] = exp.Format("2006-01-02")
			out["app_profile_days_left"] = int(math.Ceil(time.Until(exp).Hours() / 24))
		}
	}
	return out
}

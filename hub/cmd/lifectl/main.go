// lifectl: tiny CLI for Claude sessions (and the owner) to talk to the hub.
// `lifectl` alone prints every command (usageText; a test keeps it whole).
// Reads ops/hub.json `public_host` for the address and ops/secrets/hub.token
// for auth. Writes are stamped with actor(): claude:thread:$LIFE_THREAD_ID
// from a session, else owner. Flag names only ever gain aliases (cal set --on,
// goal note --actor): a running session's command keeps
// working.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// opsDir is <repo>/ops: the binary lives at <repo>/ops/bin/lifectl (make
// build), so it is two levels up from the executable when hub.json is there;
// otherwise the repo's fixed home, ~/life/ops.
func opsDir() string {
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			d := filepath.Dir(filepath.Dir(exe))
			if _, err := os.Stat(filepath.Join(d, "hub.json")); err == nil {
				return d
			}
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "life", "ops")
}

func main() {
	// Dates are Eastern here, whatever TZ
	// the calling shell carries; the hub pins the same zone (store.UseEastern).
	if loc, err := time.LoadLocation("America/New_York"); err == nil {
		time.Local = loc
	}
	if len(os.Args) < 2 {
		usage()
	}
	if os.Args[1] == "feed" {
		// Plumbing for the thread runner, no hub access needed.
		if len(os.Args) < 3 {
			die("usage: lifectl feed <file>")
		}
		feed(os.Args[2])
		return
	}
	// hub.json `public_host` is the one place the address lives (ops/hub.sh
	// and ops/hublib.py read the same key): moving the hub is that one line.
	ops := opsDir()
	cfgB, err := os.ReadFile(filepath.Join(ops, "hub.json"))
	if err != nil {
		die("hub.json: %v", err)
	}
	var cfg struct {
		PublicHost string `json:"public_host"`
	}
	if err := json.Unmarshal(cfgB, &cfg); err != nil || cfg.PublicHost == "" {
		die("hub.json: no public_host (%v)", err)
	}
	tokB, err := os.ReadFile(filepath.Join(ops, "secrets", "hub.token"))
	if err != nil {
		die("token: %v", err)
	}
	c := client{base: "https://" + cfg.PublicHost, token: strings.TrimSpace(string(tokB))}

	switch os.Args[1] {
	case "propose":
		fs := flag.NewFlagSet("propose", flag.ExitOnError)
		kind := fs.String("kind", "other", "money|delete|contact|share|commit|other")
		title := fs.String("title", "", "one line")
		detail := fs.String("detail", "", "what exactly will happen")
		project := fs.String("project", "", "project name")
		exec := fs.String("exec", "none", "none|shell|claude")
		payload := fs.String("payload", "{}", "exec payload JSON")
		gated := fs.Bool("gated", false, "force approval even for kind=other")
		thread := fs.String("thread", os.Getenv("LIFE_THREAD_ID"), "proposing session id (default $LIFE_THREAD_ID); the decision is sent back to it")
		say := fs.String("say", "", "the message the headphones speak when the card lands and the card leads with (≤700 chars); none = the hub writes one")
		fs.Parse(os.Args[2:])
		src := "lifectl"
		if *thread != "" {
			src = "claude:thread:" + *thread
		}
		body := map[string]any{"kind": *kind, "title": *title, "detail": text(*detail), "project": *project,
			"exec_type": *exec, "exec_payload": json.RawMessage(*payload), "gated": *gated, "thread_id": *thread,
			"say": *say, "run_id": os.Getenv("LIFE_RUN_ID"), "source": src}
		c.call("POST", "/api/v1/actions", body)
	case "relay":
		// Hand a task or a fact to ANOTHER session. Always an
		// approval card: the owner's Approve is what delivers the words, and nothing
		// else can (threads/relay.go). The card shows exactly what is sent.
		//   lifectl relay <session-id> "text|@file" [--why "one line: why that session needs it"]
		need(4)
		fs := flag.NewFlagSet("relay", flag.ExitOnError)
		why := fs.String("why", "", "one line under the words on the card")
		say := fs.String("say", "", "one sentence the headphones speak when the card lands (kept on the card)")
		thread := fs.String("thread", os.Getenv("LIFE_THREAD_ID"), "the session handing it over (default $LIFE_THREAD_ID)")
		fs.Parse(os.Args[4:])
		if *thread == "" {
			die("relay: only a session hands a task to another session — you just message it")
		}
		payload, _ := json.Marshal(map[string]string{"to": os.Args[2], "text": text(titleArg(os.Args[3]))})
		c.call("POST", "/api/v1/actions", map[string]any{"kind": "other", "detail": text(*why), "exec_type": "relay",
			"exec_payload": json.RawMessage(payload), "gated": true, "thread_id": *thread, "say": *say,
			"run_id": os.Getenv("LIFE_RUN_ID"), "source": "claude:thread:" + *thread})
	case "api":
		// Any hub endpoint, with the token attached — the replacement for
		// `curl -s`, which is off prompt_tools because it could just as
		// easily POST the owner's numbers to any host.
		// This one can only ever reach the hub.
		if len(os.Args) < 4 {
			die("usage: lifectl api GET|POST|PATCH|DELETE /api/v1/… [json|@file|-]")
		}
		method := strings.ToUpper(os.Args[2])
		path := os.Args[3]
		if !strings.HasPrefix(path, "/") {
			die("path starts with / — it is a hub path, not a URL (host is fixed)")
		}
		var body any
		if len(os.Args) > 4 {
			raw := text(os.Args[4])
			if !json.Valid([]byte(raw)) {
				die("body is not valid JSON")
			}
			body = json.RawMessage(raw)
		}
		c.call(method, path, body)
	case "actions":
		state := ""
		if len(os.Args) > 2 {
			state = os.Args[2]
		}
		c.call("GET", "/api/v1/actions?state="+state+"&limit=50", nil)
	case "approve", "deny", "dismiss", "reopen":
		if len(os.Args) < 3 {
			usage()
		}
		// optional trailing words = a note relayed to the proposing session
		// (a dismiss says nothing to anyone: no note)
		var body any
		if len(os.Args) > 3 && (os.Args[1] == "approve" || os.Args[1] == "deny") {
			body = map[string]string{"message": strings.Join(os.Args[3:], " ")}
		}
		c.call("POST", "/api/v1/actions/"+os.Args[2]+"/"+os.Args[1]+"?via=cli", body)
	case "push":
		// lifectl push test → APNs test alert to the phone (204 = delivered to Apple).
		c.call("POST", "/api/v1/devices/test", nil)
	case "jobs":
		c.call("GET", "/api/v1/schedule", nil)
	case "run":
		if len(os.Args) < 3 {
			usage()
		}
		c.call("POST", "/api/v1/schedule/"+os.Args[2]+"/run", nil)
	case "runs":
		job := ""
		if len(os.Args) > 2 {
			job = os.Args[2]
		}
		c.call("GET", "/api/v1/runs?job="+job+"&limit=20", nil)
	case "reload":
		c.call("POST", "/api/v1/schedule/reload", nil)
	case "status":
		c.call("GET", "/api/v1/status", nil)
	case "goals":
		c.call("GET", "/api/v1/goals", nil)
	case "goal":
		// lifectl goal <id> [--full]   show goal + digest + state notes newer than it + newest 5 notes (--full: 50)
		// lifectl goal <id> digest "..."  rewrite the goal's state summary
		// lifectl goal <id> note "..."  add a note (author owner, or --author claude:<job>)
		// lifectl goal new "title" "statement" [--horizon h] [--cadence c] [--sources s]
		if len(os.Args) < 3 {
			usage()
		}
		if os.Args[2] == "new" {
			fs := flag.NewFlagSet("goal new", flag.ExitOnError)
			horizon := fs.String("horizon", "ongoing", "ongoing|year|quarter|month")
			cadence := fs.String("cadence", "weekly", "review cadence")
			sources := fs.String("sources", "", "comma-separated data sources")
			need(5)
			fs.Parse(os.Args[5:])
			c.call("POST", "/api/v1/goals", map[string]string{"title": titleArg(os.Args[3]), "statement": os.Args[4], "horizon": *horizon, "cadence": *cadence, "sources": *sources})
			return
		}
		// A goal's statement is what every session reads before it acts, so
		// widening one has to be doable from here and not only from the app.
		// The digest is the goal's current state in flat prose — what a session
		// reads INSTEAD of 20 notes. Rewrite it (it supersedes, it is not
		// append-only); the notes below it stay the history.
		if len(os.Args) >= 5 && os.Args[3] == "digest" {
			c.call("PATCH", "/api/v1/goals/"+os.Args[2], map[string]string{"digest": text(os.Args[4])})
			return
		}
		if len(os.Args) >= 6 && os.Args[3] == "set" {
			switch os.Args[4] {
			case "title", "statement", "horizon", "cadence", "status", "sources", "digest":
			default:
				fmt.Fprintln(os.Stderr, "settable: title|statement|horizon|cadence|status|sources|digest")
				os.Exit(2)
			}
			c.call("PATCH", "/api/v1/goals/"+os.Args[2], map[string]string{os.Args[4]: os.Args[5]})
			return
		}
		if len(os.Args) >= 5 && os.Args[3] == "note" {
			// lifectl goal <id> note "<text>|@file" [--author A|--actor A] [--kind K]
			// The old positional form `note "<text>" <author> <kind>` still
			// works; before 2026-09-26 `--author x` was taken as the author
			// "--author" and the kind "x".
			author, kind := actor(), "note"
			var pos []string
			for i := 5; i < len(os.Args); i++ {
				a := os.Args[i]
				name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
				if !strings.HasPrefix(a, "--") {
					pos = append(pos, a)
					continue
				}
				if !hasVal {
					if i+1 >= len(os.Args) {
						die("goal note: %s needs a value", a)
					}
					i++
					val = os.Args[i]
				}
				switch name {
				case "author", "actor", "by":
					author = val
				case "kind":
					kind = val
				default:
					die("goal note: unknown flag %s (--author, --kind)", a)
				}
			}
			if len(pos) >= 1 {
				author = pos[0]
			}
			if len(pos) >= 2 {
				kind = pos[1]
			}
			c.call("POST", "/api/v1/goals/"+os.Args[2]+"/notes", map[string]string{"author": author, "kind": kind, "text": text(os.Args[4])})
			return
		}
		// Default read is the digest, every decision/context/intent note the
		// digest predates, and the newest 5 notes. The full history is 12-29 KB
		// per goal and it lands in a context that is re-read on EVERY turn, so
		// it is opt-in: `--full`. The since-digest part is not: without it a
		// session reading an old digest + five daily logs misses a decision
		// made in between.
		notes := 5
		if len(os.Args) >= 4 && (os.Args[3] == "--full" || os.Args[3] == "full") {
			notes = 50
		}
		c.call("GET", "/api/v1/goals/"+os.Args[2], nil)
		fmt.Println("\n# decisions, context and intents newer than the digest (the digest does not know these)")
		c.call("GET", "/api/v1/goals/"+os.Args[2]+"/notes?since=digest&limit=40", nil)
		fmt.Println("\n# newest notes")
		c.call("GET", "/api/v1/goals/"+os.Args[2]+fmt.Sprintf("/notes?limit=%d", notes), nil)
	case "obs":
		// lifectl obs [kind]         list recent observations
		// lifectl obs counts         what data exists
		// lifectl obs add <source> <kind> '<json payload>' [ts]   (ts = RFC3339 or YYYY-MM-DD, back-dates the observation)
		if len(os.Args) >= 3 && os.Args[2] == "counts" {
			c.call("GET", "/api/v1/observations/counts", nil)
		} else if len(os.Args) >= 6 && os.Args[2] == "add" {
			body := map[string]any{"source": os.Args[3], "kind": os.Args[4], "payload": json.RawMessage(readJSONArg(os.Args[5]))}
			if len(os.Args) >= 7 {
				ts := os.Args[6]
				if d, err := time.ParseInLocation("2006-01-02", ts, time.Local); err == nil {
					ts = d.Add(12 * time.Hour).Format(time.RFC3339)
				}
				body["ts"] = ts
			}
			c.call("POST", "/api/v1/observations", body)
		} else {
			kind := ""
			if len(os.Args) >= 3 {
				kind = os.Args[2]
			}
			c.call("GET", "/api/v1/observations?kind="+kind+"&limit=30", nil)
		}
	case "prompt":
		// The one way to say something to a session — this one, a new one, now
		// or later.
		//   lifectl prompt "text"                     → this session, now
		//   lifectl prompt "text" --in 30m            → this session, in 30 min (check back on yourself)
		//   lifectl prompt "text" --on 2026-09-03 --at 09:00
		//   lifectl prompt "text" --new [--title ...] → a fresh session (a parallel worker)
		//   lifectl prompt "text" --re ask:ask-1234 [--outcome done]
		//   lifectl prompts [queued] / lifectl prompt cancel <id>
		need(3)
		if os.Args[2] == "cancel" {
			need(4)
			c.call("POST", "/api/v1/prompts/"+os.Args[3]+"/cancel", nil)
			return
		}
		fs := flag.NewFlagSet("prompt", flag.ExitOnError)
		in := fs.String("in", "", "deliver after a delay: 30m, 2h")
		on := fs.String("on", "", "deliver on YYYY-MM-DD (Eastern)")
		at := fs.String("at", "", "HH:MM with --on (default 09:00), or an RFC3339 instant on its own")
		newT := fs.Bool("new", false, "start a fresh session instead of prompting this one")
		title := fs.String("title", "", "title for the session --new starts")
		goal := fs.String("goal", "", "goal id for the session --new starts")
		re := fs.String("re", "", "what this answers: ask:<id> | action:<id> | rec:<id> | cal:<id> | message:<id>")
		outcome := fs.String("outcome", "", "done|wont (about an ask), approved|denied (about a proposal)")
		thread := fs.String("thread", os.Getenv("LIFE_THREAD_ID"), "session to prompt (default $LIFE_THREAD_ID)")
		fs.Parse(os.Args[3:])
		author := actor()
		target := *thread
		if *newT {
			target = "new"
		}
		if target == "" {
			die("prompt: --thread required outside a session (or --new)")
		}
		c.call("POST", "/api/v1/prompts", map[string]any{"author": author, "target": target, "text": titleArg(os.Args[2]),
			"in": *in, "on": *on, "at": *at, "title": *title, "goal_id": *goal, "in_reply_to": *re, "outcome": *outcome})
	case "prompts":
		// lifectl prompts [queued|delivered|cancelled|failed] [--thread id]
		state := ""
		if len(os.Args) > 2 && !strings.HasPrefix(os.Args[2], "--") {
			state = os.Args[2]
		}
		thread := threadFlag(os.Getenv("LIFE_THREAD_ID"))
		c.call("GET", "/api/v1/prompts?"+url.Values{"state": {state}, "thread": {thread}}.Encode(), nil)
	case "asks":
		// lifectl asks [active|all|open|answered|done|dismissed] [--thread id]
		state := ""
		if len(os.Args) > 2 && !strings.HasPrefix(os.Args[2], "--") {
			state = os.Args[2]
		}
		thread := threadFlag("")
		c.call("GET", "/api/v1/asks?"+url.Values{"state": {state}, "thread": {thread}}.Encode(), nil)
	case "ask":
		// lifectl ask add "title" [--detail ...] [--kind decision|access|physical|read|install|other] [--surface mobile|web|any] [--check ...] [--thread id] [--on YYYY-MM-DD [--at HH:MM]]
		// lifectl ask <id>                      show
		// lifectl ask <id> done|dismiss|reopen ["what happened"]
		// lifectl ask <id> surface mobile|web|any
		// lifectl ask <id> kind decision|access|physical|read|install|other
		need(3)
		if os.Args[2] == "add" {
			need(4)
			fs := flag.NewFlagSet("ask add", flag.ExitOnError)
			detail := fs.String("detail", "", "what was tried, why blocked, what unblocks (@/path or - reads it from a file/stdin)")
			kind := fs.String("kind", "other", "decision|access|physical|read|install|other")
			check := fs.String("check", "", "how a verifier could tell it is done")
			// Where it gets done. Default "" = infer from the text
			// (threads/surface.go): a console URL or an API key reads as web,
			// an itms-services link as mobile, everything else as any.
			surface := fs.String("surface", "", "mobile = done on the phone | web = done at the Mac (quiet on the phone) | any (default, inferred)")
			thread := fs.String("thread", os.Getenv("LIFE_THREAD_ID"), "thread id (default $LIFE_THREAD_ID)")
			say := fs.String("say", "", "THE message (the headphones speak it and the card leads with it): stands alone, first person, the whole answer in 2-5 plain sentences, ≤700 chars; --detail then holds only the owner's steps")
			on := fs.String("on", "", "YYYY-MM-DD: hold it off the board until that day (it becomes an ask that morning)")
			at := fs.String("at", "", "HH:MM with --on (default 08:00)")
			fs.Parse(os.Args[4:])
			if *thread == "" {
				die("ask add: --thread required outside a session")
			}
			if *on != "" {
				// Dated ask: a calendar item that mints exactly this ask on the
				// day. Nothing sits on the owner's board in the meantime — the thing
				// only becomes theirs when it is actually doable.
				src := actor()
				c.call("POST", "/api/v1/calendar", map[string]any{"title": titleArg(os.Args[3]), "day": *on, "at": *at, "kind": "owner",
					"detail": text(*detail), "thread_id": *thread, "source": src, "ask_kind": *kind, "check_hint": *check, "surface": *surface, "say": *say})
				return
			}
			c.call("POST", "/api/v1/asks", map[string]string{"thread_id": *thread, "run_id": os.Getenv("LIFE_RUN_ID"), "title": titleArg(os.Args[3]), "detail": text(*detail), "kind": *kind, "check": *check, "surface": *surface, "say": *say})
			return
		}
		id := os.Args[2]
		if len(os.Args) == 3 {
			c.call("GET", "/api/v1/asks/"+id, nil)
			return
		}
		if os.Args[3] == "surface" {
			need(5)
			c.call("POST", "/api/v1/asks/"+id+"/surface", map[string]string{"surface": os.Args[4]})
			return
		}
		if os.Args[3] == "kind" {
			need(5)
			c.call("POST", "/api/v1/asks/"+id+"/kind", map[string]string{"kind": os.Args[4]})
			return
		}
		state := map[string]string{"done": "done", "dismiss": "dismissed", "dismissed": "dismissed", "reopen": "open", "open": "open"}[os.Args[3]]
		if state == "" {
			usage()
		}
		by := actor()
		note := ""
		if len(os.Args) > 4 {
			note = strings.Join(os.Args[4:], " ")
		}
		c.call("POST", "/api/v1/asks/"+id+"/resolve", map[string]string{"state": state, "by": by, "note": note})
	case "calendar", "cal":
		// lifectl cal [from] [to]                      agenda (default today → +60d)
		// lifectl cal add "title" --on YYYY-MM-DD [--at HH:MM] [--kind owner|homework|agent|note] [--detail d] [--goal g] [--thread id] [--repeat daily|weekly|monthly|yearly] [--nag minutes]
		// lifectl cal <id>                             show
		// lifectl cal <id> done|dismiss|reopen ["what happened"]
		// lifectl cal add "title" [--detail d] [--goal g]   NO --on = a soon item: the owner's to-do with no due day, never overdue
		// lifectl cal <id> soon                        an open owner step loses its day and becomes a soon item
		if len(os.Args) > 2 && os.Args[2] == "add" {
			need(4)
			fs := flag.NewFlagSet("cal add", flag.ExitOnError)
			on := fs.String("on", "", "YYYY-MM-DD (local)")
			at := fs.String("at", "", "HH:MM (owner/homework/note: default all day, the ask is raised 08:00; agent: always a minute — default 08:00, then 30 min past the other agent runs that day — so give the time it should run)")
			kind := fs.String("kind", "owner", "owner = the owner's step (becomes an ask that day, nagged until done) | homework = a Learn step of the owner's, same lifecycle, its own light-blue lane | agent = run an agent with --detail as its instructions | note = reminder (read ask)")
			detail := fs.String("detail", "", "what exactly, and why (@/path or - reads it from a file/stdin)")
			goal := fs.String("goal", "", "goal id")
			thread := fs.String("thread", os.Getenv("LIFE_THREAD_ID"), "session the ask/run lands in (default $LIFE_THREAD_ID)")
			repeat := fs.String("repeat", "", "daily|weekly|monthly|yearly|every<N>d (e.g. every2d)")
			nag := fs.Int("nag", 0, "minutes between reminders after it is due (owner items; default 360)")
			due := fs.String("due", "", "owner/homework: on = its day only, missed at midnight, never overdue (a chore: water the plants) | by = any time up to its day, overdue after (an assignment, a deadline). Default: on for --repeat daily, else by")
			fs.Parse(os.Args[4:])
			// No --on = a SOON item: a to-do of the owner's with no due day.
			// It is never overdue, never nagged, raises no ask, and belongs to no
			// session (the hub drops thread_id too; `source` says who filed it).
			soon := *on == ""
			if soon {
				if *kind != "owner" || *at != "" || *repeat != "" {
					die("cal add: --on YYYY-MM-DD required (only a plain --kind owner to-do may leave it off: no --at, no --repeat)")
				}
				*thread = ""
			}
			src := actor()
			c.call("POST", "/api/v1/calendar", map[string]any{"title": titleArg(os.Args[3]), "day": *on, "soon": soon, "at": *at, "kind": *kind, "detail": text(*detail),
				"goal_id": *goal, "thread_id": *thread, "repeat": *repeat, "nag_min": *nag, "source": src, "due": *due})
			return
		}
		if len(os.Args) > 2 && strings.HasPrefix(os.Args[2], "cal-") {
			id := os.Args[2]
			if len(os.Args) == 3 {
				c.call("GET", "/api/v1/calendar/"+id, nil)
				return
			}
			if os.Args[3] == "set" {
				// lifectl cal <id> set [--title t] [--detail d|@file] [--day YYYY-MM-DD] [--at HH:MM] [--repeat r]
				fs := flag.NewFlagSet("cal set", flag.ExitOnError)
				title := fs.String("title", "", "new title")
				detail := fs.String("detail", "", "new detail (@/path or - reads it from a file/stdin)")
				day := fs.String("day", "", "YYYY-MM-DD (local), or \"none\" for soon: no due day, never overdue")
				fs.StringVar(day, "on", "", "alias of --day (the flag `cal add` takes)")
				at := fs.String("at", "", "HH:MM, or \"none\" for all-day")
				repeat := fs.String("repeat", "", "daily|weekly|monthly|yearly|every<N>d, or \"none\" to stop repeating")
				due := fs.String("due", "", "on (its day only) | by (any time up to its day)")
				fs.Parse(os.Args[4:])
				body := map[string]string{}
				put := func(k, v string, prose bool) {
					if v == "" {
						return
					}
					if v == "none" && (k == "at" || k == "repeat" || k == "day") {
						v = ""
					} else if prose {
						v = text(v)
					}
					body[k] = v
				}
				put("title", *title, false)
				put("detail", *detail, true)
				put("day", *day, false)
				put("at", *at, false)
				put("repeat", *repeat, false)
				put("due", *due, false)
				if len(body) == 0 {
					die("cal set: nothing to change")
				}
				c.call("PATCH", "/api/v1/calendar/"+id, body)
				return
			}
			if os.Args[3] == "soon" {
				// lifectl cal <id> soon — an open step of the owner's loses its day: a
				// to-do, never overdue, off its session (the ask it had raised
				// closes by the hub; nobody is woken).
				c.call("PATCH", "/api/v1/calendar/"+id, map[string]string{"day": ""})
				return
			}
			state := map[string]string{"done": "done", "dismiss": "dismissed", "dismissed": "dismissed", "reopen": "scheduled", "scheduled": "scheduled"}[os.Args[3]]
			if state == "" {
				usage()
			}
			by := actor()
			note := ""
			if len(os.Args) > 4 {
				note = strings.Join(os.Args[4:], " ")
			}
			c.call("POST", "/api/v1/calendar/"+id+"/resolve", map[string]string{"state": state, "by": by, "note": note})
			return
		}
		from, to := "", ""
		if len(os.Args) > 2 {
			from = os.Args[2]
		}
		if len(os.Args) > 3 {
			to = os.Args[3]
		}
		c.call("GET", "/api/v1/calendar?from="+from+"&to="+to, nil)
	case "recs":
		// lifectl recs [open|all|deferred|accepted|declined|expired] [--domain d] [--kind k] [--goal g] [--due YYYY-MM-DD] [--under N] [--model id]
		// lifectl recs stats                            the track record
		//
		// Recommendations are PULL, not push: adding one notifies nobody, it
		// waits on this list. Asks are the push channel.
		if len(os.Args) > 2 && os.Args[2] == "stats" {
			c.call("GET", "/api/v1/recs/stats", nil)
			return
		}
		fs := flag.NewFlagSet("recs", flag.ExitOnError)
		domain := fs.String("domain", "", "money|health|audience|tools|home|other")
		kind := fs.String("kind", "", "buy|subscribe|trade|try|stop|habit|process|other")
		goal := fs.String("goal", "", "goal id")
		due := fs.String("due", "", "YYYY-MM-DD: accepted recs whose review date has arrived and are not scored")
		under := fs.Float64("under", 0, "only recs costing at most this many dollars")
		model := fs.String("model", "", "only recs filed by this model id (claude-opus-5)")
		state := ""
		rest := os.Args[2:]
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			state, rest = rest[0], rest[1:]
		}
		fs.Parse(rest)
		q := url.Values{"status": {state}, "domain": {*domain}, "kind": {*kind}, "goal": {*goal}, "due": {*due}, "model": {*model}}
		if *under > 0 {
			q.Set("max_cents", strconv.Itoa(int(*under*100+0.5)))
		}
		c.call("GET", "/api/v1/recs?"+q.Encode(), nil)
	case "rec":
		// lifectl rec add "title" [--detail d] [--domain d] [--kind k] [--cost 69.00] [--period monthly|yearly]
		//                         [--effort low|med|high] [--confidence 0-100] [--because "the evidence"]
		//                         [--expect "what changes and how we would know"] [--act-by DATE] [--review-on DATE]
		//                         [--goal g] [--supersedes rec-id] [--model claude-opus-5]
		// lifectl rec <id>                              show
		// lifectl rec <id> accept|decline|done|reopen ["the reason"]
		// lifectl rec <id> defer YYYY-MM-DD ["why wait"]   neither yes nor no: off the list until that day, then back + an agent check-in
		// lifectl rec <id> score worked|mixed|failed|unclear ["what actually happened"]
		// lifectl rec <id> link cal-1234 ask-ab12       what accepting it minted
		need(3)
		if os.Args[2] == "add" {
			need(4)
			fs := flag.NewFlagSet("rec add", flag.ExitOnError)
			detail := fs.String("detail", "", "the case for it, in the markdown subset")
			domain := fs.String("domain", "other", "money|health|audience|tools|home|other")
			kind := fs.String("kind", "other", "buy|subscribe|trade|try|stop|habit|process|other")
			cost := fs.Float64("cost", 0, "dollars it costs to say yes")
			period := fs.String("period", "", "monthly|yearly (empty = one-off)")
			effort := fs.String("effort", "low", "low|med|high — of the owner's time")
			conf := fs.Int("confidence", 50, "0..100, your own prior that this is right")
			because := fs.String("because", "", "the evidence that produced it")
			expect := fs.String("expect", "", "what should change, and how we would know")
			actBy := fs.String("act-by", "", "YYYY-MM-DD after which it is stale (auto-expires)")
			reviewOn := fs.String("review-on", "", "YYYY-MM-DD to come back and score it (default: +30d on accept)")
			goal := fs.String("goal", "", "goal id")
			prev := fs.String("supersedes", "", "rec id this replaces (closes it)")
			model := fs.String("model", "", "model id that wrote it (claude-opus-5); default: the hub reads it off this session's run")
			fs.Parse(os.Args[4:])
			src := actor()
			c.call("POST", "/api/v1/recs", map[string]any{"title": titleArg(os.Args[3]), "detail": text(*detail), "domain": *domain,
				"kind": *kind, "cost_cents": int(*cost*100 + 0.5), "cost_period": *period, "effort": *effort,
				"confidence": *conf, "because": text(*because), "expect": text(*expect), "act_by": *actBy, "review_on": *reviewOn,
				"goal_id": *goal, "thread_id": os.Getenv("LIFE_THREAD_ID"), "source": src, "prev_id": *prev, "model": *model})
			return
		}
		id := os.Args[2]
		if len(os.Args) == 3 {
			c.call("GET", "/api/v1/recs/"+id, nil)
			return
		}
		by := actor()
		switch os.Args[3] {
		case "score":
			need(5)
			c.call("POST", "/api/v1/recs/"+id+"/score", map[string]string{"outcome": os.Args[4], "by": by,
				"note": strings.Join(os.Args[5:], " ")})
		case "link":
			need(5)
			c.call("POST", "/api/v1/recs/"+id+"/link", map[string]any{"refs": os.Args[4:]})
		case "starter":
			// What an agent is told when the owner decides on it — the brief a new
			// session opens with, and the relay the filing session gets.
			c.call("GET", "/api/v1/recs/"+id+"/starter", nil)
		case "defer":
			need(5)
			c.call("POST", "/api/v1/recs/"+id+"/decide", map[string]string{"status": "deferred", "until": os.Args[4], "by": by,
				"note": strings.Join(os.Args[5:], " ")})
		default:
			status := map[string]string{"accept": "accepted", "accepted": "accepted", "decline": "declined",
				"declined": "declined", "done": "done", "reopen": "proposed", "expire": "expired"}[os.Args[3]]
			if status == "" {
				usage()
			}
			c.call("POST", "/api/v1/recs/"+id+"/decide", map[string]string{"status": status, "by": by,
				"note": strings.Join(os.Args[4:], " ")})
		}
	case "threads":
		c.call("GET", "/api/v1/threads", nil)
	case "thread":
		// lifectl thread new "<prompt>" [--goal id] [--schedule when]
		// lifectl thread <id>                 show
		// lifectl thread <id> send "<text>"
		// lifectl thread <id> schedule <when|off>
		// lifectl thread <id> goal <goal-id|off>
		// lifectl thread <id> model <auto|judgment|build|claude-…>
		// lifectl thread <id> checkin | stop | archive | events
		if len(os.Args) < 3 {
			usage()
		}
		if os.Args[2] == "new" {
			need(4)
			fs := flag.NewFlagSet("thread new", flag.ExitOnError)
			goal := fs.String("goal", "", "goal id")
			sched := fs.String("schedule", "", "daily@HH:MM|weekly@Mon HH:MM|every@6h")
			title := fs.String("title", "", "title")
			project := fs.String("project", "life", "project")
			fs.Parse(os.Args[4:])
			c.call("POST", "/api/v1/threads", map[string]string{"prompt": titleArg(os.Args[3]), "goal_id": *goal, "schedule": *sched, "title": *title, "project": *project})
			return
		}
		id := os.Args[2]
		if len(os.Args) == 3 {
			c.call("GET", "/api/v1/threads/"+id, nil)
			c.call("GET", "/api/v1/threads/"+id+"/messages?limit=20", nil)
			return
		}
		switch os.Args[3] {
		case "send":
			need(5)
			c.call("POST", "/api/v1/threads/"+id+"/messages", map[string]string{"text": text(os.Args[4])})
		case "schedule":
			need(5)
			when := os.Args[4]
			if when == "off" {
				when = ""
			}
			// lifectl thread <id> schedule <when|off> ["what to do at each check-in"]
			body := map[string]string{"schedule": when}
			if len(os.Args) > 5 {
				// A standing prompt is the longest string this tool takes and
				// the one a shell is worst at carrying (quotes, $, apostrophes
				// in prose), so it reads @/path and "-" like every --detail.
				body["schedule_prompt"] = text(os.Args[5])
			}
			c.call("PATCH", "/api/v1/threads/"+id, body)
		case "goal":
			// lifectl thread <id> goal <goal-id|off>  — agent-chosen primary goal
			need(5)
			g := os.Args[4]
			if g == "off" {
				g = ""
			}
			c.call("PATCH", "/api/v1/threads/"+id, map[string]string{"goal_id": g})
		case "model":
			// lifectl thread <id> model <auto|judgment|build|claude-…>  — which
			// rung this thread's wakes start on (ops/hub.json model_policy)
			need(5)
			c.call("PATCH", "/api/v1/threads/"+id, map[string]string{"model_class": os.Args[4]})
		case "checkin":
			c.call("POST", "/api/v1/threads/"+id+"/checkin", nil)
		case "stop":
			c.call("POST", "/api/v1/threads/"+id+"/stop", nil)
		case "archive":
			c.call("POST", "/api/v1/threads/"+id+"/archive", nil)
		case "events":
			// lifectl thread <id> events — streamed tool calls / thinking of its runs
			c.call("GET", "/api/v1/threads/"+id+"/events?limit=200", nil)
		default:
			usage()
		}
	case "spend":
		days := "30"
		if len(os.Args) > 2 {
			days = os.Args[2]
		}
		c.call("GET", "/api/v1/spend/summary?days="+days, nil)
	case "budget":
		// lifectl budget          — today's meter: spent / budget, level, whether unattended wakes run
		// lifectl budget clear    — lift today's pause (the owner's call)
		if len(os.Args) > 2 && os.Args[2] == "clear" {
			c.call("POST", "/api/v1/spend/budget/clear", nil)
			return
		}
		c.call("GET", "/api/v1/spend/budget", nil)
	default:
		usage()
	}
}

type client struct{ base, token string }

func (c client) call(method, path string, body any) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	timeout := 30 * time.Second
	// A sync endpoint may crawl many pages. Matched without the query.
	route, _, _ := strings.Cut(path, "?")
	if strings.HasSuffix(route, "/sync") {
		timeout = 15 * time.Minute
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		die("%v", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	var pretty bytes.Buffer
	if json.Indent(&pretty, out, "", "  ") == nil {
		out = pretty.Bytes()
	}
	fmt.Println(string(out))
	if resp.StatusCode >= 300 {
		os.Exit(1)
	}
}

// actor is who is acting: the calling session (claude:thread:<LIFE_THREAD_ID>)
// or, from a plain shell, the owner ("owner", the wire value) — the one `source`/`by`/`author` every write sends.
func actor() string {
	if t := os.Getenv("LIFE_THREAD_ID"); t != "" {
		return "claude:thread:" + t
	}
	return "owner"
}

// threadFlag is the value after a `--thread` (or `--thread=`) anywhere in
// the arguments, for the list commands that take a state word before it.
func threadFlag(def string) string {
	for i, a := range os.Args {
		if a == "--thread" && i+1 < len(os.Args) {
			def = os.Args[i+1]
		} else if v, ok := strings.CutPrefix(a, "--thread="); ok {
			def = v
		}
	}
	return def
}

func die(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...); os.Exit(1) }

// need exits with usage unless at least n args were given.
func need(n int) {
	if len(os.Args) < n {
		usage()
	}
}

// titleArg is a positional title (`ask add "…"`, `cal add "…"`, `rec add "…"`).
// Every one of these commands takes the title BEFORE its flags, and Go's flag
// package stops parsing at the first non-flag word — so `lifectl ask add --kind
// read --title "…"` silently filed an ask titled "--kind" with no detail and
// the default kind, which reached the owner's board as a bare red card.
// A title starting with "-" is always that mistake; refuse it.
func titleArg(s string) string {
	if strings.HasPrefix(s, "-") {
		die("lifectl: the title comes first, before the flags — got %q as the title.\n"+
			"       e.g. lifectl ask add \"Install build 380\" --kind read --surface web", s)
	}
	return s
}

// text returns a prose argument given inline, as @/path (file) or "-" (stdin).
// Long markdown details are the norm here and a shell cannot always carry one
// on the command line, so every --detail-shaped flag runs through this.
func text(s string) string {
	var b []byte
	var err error
	switch {
	case s == "-":
		b, err = io.ReadAll(os.Stdin)
	case strings.HasPrefix(s, "@") && isFile(strings.TrimPrefix(s, "@")):
		// Only a path that exists is a file: "@mentions in a message" or
		// "@handle" is prose and goes through as written.
		b, err = os.ReadFile(strings.TrimPrefix(s, "@"))
	default:
		return s
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lifectl: read text:", err)
		os.Exit(2)
	}
	return strings.TrimRight(string(b), "\n")
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// readJSONArg returns a JSON argument given inline, as @/path (file) or "-" (stdin).
func readJSONArg(arg string) []byte {
	var b []byte
	var err error
	switch {
	case arg == "-":
		b, err = io.ReadAll(os.Stdin)
	case strings.HasPrefix(arg, "@"):
		b, err = os.ReadFile(strings.TrimPrefix(arg, "@"))
	default:
		return []byte(arg)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lifectl: read json:", err)
		os.Exit(2)
	}
	if !json.Valid(b) {
		fmt.Fprintln(os.Stderr, "lifectl: argument is not valid JSON")
		os.Exit(2)
	}
	return b
}

// usageText lists every command (TestUsageListsEveryCommand keeps it so).
// Titles come BEFORE flags; any prose flag takes text, @file or - (stdin).
const usageText = `usage: lifectl <command> …   (reads ops/hub.json public_host + ops/secrets/hub.token)

hub       api METHOD /api/v1/… [json|@file|-] · status · reload · jobs · run <job> · runs [job]
          push test · spend [days] · budget [clear] · feed <file>
gate      propose --title t --detail d [--kind money|delete|contact|share|commit|other] [--gated]
            [--exec none|shell|claude --payload json] [--project p] [--thread id] [--say "…"]
          actions [state] · approve|deny|dismiss|reopen <id> [note…]
          relay <session-id> "text|@file" [--why "…"] [--say "…"] [--thread id]
sessions  threads · thread new "prompt" [--goal g] [--schedule w] [--title t] [--project p]
          thread <id> [send text|@file|- | schedule when|off ["check-in prompt"] | goal g|off
            | model auto|judgment|build|claude-… | checkin | stop | archive | events]
          prompt "text" [--thread id | --new [--title t] [--goal g]] [--in 30m | --on YYYY-MM-DD [--at HH:MM]]
            [--re ask:id|action:id|rec:id|cal:id|message:id] [--outcome done|wont|approved|denied]
          prompt cancel <id> · prompts [queued|delivered|cancelled|failed] [--thread id]
asks      asks [active|all|open|answered|done|dismissed] [--thread id]
          ask add "title" [--detail d] [--kind decision|access|physical|read|install|other]
            [--surface mobile|web|any] [--check c] [--say "…"] [--thread id] [--on YYYY-MM-DD [--at HH:MM]]
          ask <id> [done|dismiss|reopen [note] | surface mobile|web|any | kind k]
calendar  cal [from] [to]
          cal add "title" [--on YYYY-MM-DD] [--at HH:MM] [--kind owner|homework|agent|note] [--due on|by]
            [--detail d] [--goal g] [--thread id] [--repeat daily|weekly|monthly|yearly|every<N>d] [--nag min]
            (no --on = soon: your to-do, no due day)
          cal <id> [done|dismiss|reopen [note] | soon
            | set [--title t] [--detail d] [--day|--on d|none] [--at HH:MM|none] [--repeat r|none] [--due on|by]]
goals     goals · goal new "title" "statement" [--horizon h] [--cadence c] [--sources s]
          goal <id> [--full] · goal <id> digest "text|@file"
          goal <id> note "text|@file" [--author|--actor a] [--kind k]   (old form: note "text" author kind)
          goal <id> set title|statement|horizon|cadence|status|sources|digest <value>
recs      recs [open|all|deferred|accepted|declined|expired] [--domain d] [--kind k] [--goal g]
            [--due YYYY-MM-DD] [--under $] [--model id] · recs stats
          rec add "title" [--detail d] [--domain d] [--kind k] [--cost $] [--period monthly|yearly]
            [--effort low|med|high] [--confidence 0-100] [--because b] [--expect e] [--act-by d]
            [--review-on d] [--goal g] [--supersedes id] [--model id]
          rec <id> [accept|decline|done|reopen|expire [note] | defer YYYY-MM-DD [note]
            | score worked|mixed|failed|unclear [note] | link ref… | starter]
data      obs [kind] · obs counts · obs add <source> <kind> json|@file|- [ts]
`

func usage() {
	fmt.Fprint(os.Stderr, usageText)
	os.Exit(2)
}

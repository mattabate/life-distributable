// One decode test per hub response type the app reads. The JSON is written by
// hand from shared/api.md — never captured from the live hub, so it holds no
// personal data and stays a statement of the contract, not of one day's
// state. Each test asserts a few fields, enough to catch
// a renamed key or a type that drifted from api.md.
import XCTest
@testable import Life

final class DecodeTests: XCTestCase {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try HubClient.decoder().decode(T.self, from: Data(json.utf8))
    }

    func testBoard() throws {
        let b = try decode(Board.self, """
        {"surface":"mobile","count":3,"working":1,
         "calendar":[],
         "sessions":[{"id":"t-1a2b3c4d","title":"Plan lunches","n":2,"first":"ask-1a2b3c4d","running":false,"actions":[],
           "asks":[{"id":"ask-1a2b3c4d","created_at":"2026-08-26T09:00:00Z","updated_at":"2026-08-26T09:00:00Z","thread_id":"t-1a2b3c4d",
                    "title":"Pick a day","detail":"Mon or Tue?","kind":"decision","verb":"decide","surface":"any","state":"open","check_hint":"","thread_running":true}],
           "steps":[]}],
         "for_you":{"t-1a2b3c4d":2},"first":{"t-1a2b3c4d":"ask-1a2b3c4d"},"todo":{"t-1a2b3c4d":"Pick a day"},
         "headings":[{"key":"your_turn","label":"Your turn","count":"2 in 1 session · 1 working","n":1,"show":0},
                     {"key":"recent","label":"Recent","count":"40 of 42","n":42,"show":40}],
         "section":{"t-1a2b3c4d":"your_turn"},
         "pills":{"t-1a2b3c4d":[{"word":"2 for you","tone":"needs"}]},
         "badges":{"your_turn":3,"calendar":0,"recs":4}}
        """)
        XCTAssertEqual(b.headings?.last?.show, 40)
        XCTAssertEqual(b.section?["t-1a2b3c4d"], "your_turn")
        XCTAssertEqual(b.pills?["t-1a2b3c4d"]?.first, Pill(word: "2 for you", tone: "needs"))
        XCTAssertEqual(b.count, 3)
        XCTAssertEqual(b.first["t-1a2b3c4d"], "ask-1a2b3c4d")
        XCTAssertEqual(b.badges.recs, 4)
        XCTAssertEqual(b.forYou["t-1a2b3c4d"], 2)
        XCTAssertEqual(b.todo?["t-1a2b3c4d"], "Pick a day")
        // The bundle is decoded now: a row draws one cell per card.
        let s = try XCTUnwrap(b.sessions?.first)
        XCTAssertEqual(s.first, "ask-1a2b3c4d")
        XCTAssertEqual(s.asks.first?.title, "Pick a day")
        let cells = SessionCell.cells(s)
        XCTAssertEqual(cells.map(\.verb), ["decide"])
        XCTAssertEqual(cells.first?.body, "Mon or Tue?")
    }

    /// The cells a session row draws are the console's `sessionCards`, rule
    /// for rule: approvals first (verb "approve"), then asks with the verb of
    /// their kind, an install or a crash with no body, a hub-minted card's
    /// body line that repeats its title dropped, and the owner's dated steps last.
    func testSessionCells() throws {
        let s = try decode(BoardSession.self, """
        {"id":"t-1","title":"Ship it","n":3,"first":"20260923-120000-ab","running":false,
         "actions":[{"id":"20260923-120000-ab","created_at":"2026-09-23T12:00:00Z","updated_at":"2026-09-23T12:00:00Z","project":"life","kind":"commit",
                     "title":"Commit the build","detail":"Two files.","gated":true,"exec_type":"shell","state":"proposed"}],
         "asks":[{"id":"ask-r","created_at":"2026-09-23T12:00:00Z","updated_at":"2026-09-23T12:00:00Z","thread_id":"t-1","title":"The plants are watered",
                  "detail":"The plants are watered\\nEvery Wednesday, 09:00.","kind":"read","verb":"read","surface":"any","state":"open","check_hint":""},
                 {"id":"ask-i","created_at":"2026-09-23T12:00:00Z","updated_at":"2026-09-23T12:00:00Z","thread_id":"t-1","title":"Install app build 1194 (tap the link)",
                  "detail":"What changed: rows.","kind":"install","verb":"install","surface":"mobile","state":"open","check_hint":""}],
         "steps":[{"id":"ask-s","created_at":"2026-09-23T12:00:00Z","updated_at":"2026-09-23T12:00:00Z","thread_id":"t-1","title":"Water the plants",
                   "detail":"","kind":"physical","verb":"do","surface":"any","state":"open","check_hint":"","cal_id":"cal-1","cal_day":"2026-09-23"}]}
        """)
        let cells = SessionCell.cells(s)
        XCTAssertEqual(cells.map(\.id), ["20260923-120000-ab", "ask-r", "ask-i", "ask-s"])
        XCTAssertEqual(cells.map(\.verb), ["approve", "read", "install", "do"])
        XCTAssertEqual(cells.map(\.tone), ["needs", "read", "install", "needs"])
        XCTAssertEqual(cells[0].body, "Two files.")
        XCTAssertEqual(cells[1].body, "Every Wednesday, 09:00.")
        XCTAssertNil(cells[2].body)
        XCTAssertNil(cells[3].body)
    }

    func testAsk() throws {
        let a = try decode(Ask.self, """
        {"id":"ask-1a2b3c4d","created_at":"2026-08-26T09:00:00.123456Z","updated_at":"2026-08-26T09:05:00Z",
         "thread_id":"t-1a2b3c4d","thread_title":"Plan lunches","run_id":"r1","message_id":7,"goal_id":"g-health",
         "title":"Install app build 402","detail":"Link: itms-services://?action=download-manifest&url=https://hub.example/ota/manifest.plist",
         "kind":"physical","surface":"mobile","state":"open","check_hint":"","thread_running":true,
         "cal_id":"cal-1a2b3c4d","cal_day":"2026-08-26","url":"https://hub.example/a/tok/ask-1a2b3c4d",
         "outcomes":[{"value":"","label":"Reply"},{"value":"done","label":"I did this"},{"value":"wont","label":"Won't do"}]}
        """)
        XCTAssertEqual(a.message_id, 7)
        XCTAssertEqual(a.outcomes?.count, 3)
        XCTAssertEqual(RespondSubject(ask: a).outcomes.map(\.label), ["Reply", "I did this", "Won't do"])
        XCTAssertEqual(a.phoneLink?.scheme, "itms-services")
        XCTAssertTrue(a.fromCalendar)
    }

    /// The install link arrives in every shape sessions actually write — bare
    /// URL, markdown [text](url) (the house rule is links-not-text), OTA page
    /// instead of manifest — and each must still make the red Install button.
    /// A markdown link once rendered as a generic card.
    func testInstallLinkShapes() throws {
        func ask(_ detail: String, kind: String = "install") throws -> Ask {
            let d = String(data: try JSONEncoder().encode(detail), encoding: .utf8)!
            return try decode(Ask.self, """
            {"id":"ask-1a2b3c4d","created_at":"2026-08-26T09:00:00Z","updated_at":"2026-08-26T09:00:00Z",
             "thread_id":"t-1a2b3c4d","title":"Install app build 808 (tap the link)",
             "detail":\(d),
             "kind":"\(kind)","surface":"mobile","state":"open","check_hint":"","thread_running":true}
            """)
        }
        let markdown = try ask("What changed, in prose.\n\n[Install build 808](https://hub.example:8443/ota/tok/install.html)")
        XCTAssertEqual(markdown.phoneLink?.scheme, "itms-services")
        XCTAssertTrue(markdown.phoneLink?.absoluteString.contains("manifest") ?? false)
        XCTAssertEqual(markdown.detailWithoutLinks, "What changed, in prose.")
        let bare = try ask("What changed, in prose.\n\nhttps://hub.example:8443/ota/tok/install.html")
        XCTAssertEqual(bare.phoneLink?.scheme, "itms-services")
        XCTAssertEqual(bare.detailWithoutLinks, "What changed, in prose.")
        // A fenced block is text the owner copies: its links stay (otherwise
        // a copy box of links arrives empty), install card or not.
        let fenced = "Tap Copy.\n\n```\nhttps://x.com/a\nhttps://x.com/b\n```"
        XCTAssertEqual(try ask(fenced).detailWithoutLinks, fenced)
        XCTAssertEqual(Ask.detailWithoutLinks(fenced), fenced)
        // Only the install card draws a link button: any other kind keeps
        // its links in the text, or they are visible nowhere.
        let list = "See https://x.com/a\n\n- [b](https://x.com/b)"
        XCTAssertEqual(try ask(list, kind: "read").detailWithoutLinks, list)
        // A console route stays buttonless even when written as markdown.
        let console = try ask("Look at [Money](https://hub.example:8443/#/money) later.")
        XCTAssertNil(console.phoneLink)
    }

    /// A run block cut at the card the agent raised mid-chain: the phone
    /// draws fold · card · fold, each fold with the hub's own count for it.
    func testStepsCarryTheirSegments() throws {
        let s = try decode([StepCount].self, """
        [{"message_id":7,"run_id":"r1","tools":40,"thoughts":4,"steps":50,"first_id":100,"last_id":200,
          "segments":[{"ref":"ask:ask-1a2b3c4d","tools":10,"thoughts":1,"steps":12,"first_id":100,"last_id":140},
                      {"ref":"action:act-1a2b3c4d","tools":5,"thoughts":0,"steps":5,"first_id":141,"last_id":160},
                      {"tools":25,"thoughts":3,"steps":33,"first_id":161,"last_id":200}]},
         {"message_id":9,"run_id":"r1","tools":1,"thoughts":0,"steps":1,"first_id":201,"last_id":201}]
        """)
        XCTAssertEqual(s.first?.segments?.count, 3)
        XCTAssertEqual(s.first?.segments?.map(\.steps).reduce(0, +), s.first?.steps)
        XCTAssertEqual(s.first?.segments?.last?.ref, nil)   // the tail: still running
        XCTAssertNil(s.last?.segments)                      // no card: one fold, as before
    }

    func testActionWithEvents() throws {
        let a = try decode(Action.self, """
        {"id":"act-1a2b3c4d","created_at":"2026-08-26T09:00:00Z","updated_at":"2026-08-26T09:02:00Z","project":"life",
         "kind":"commit","title":"git commit","detail":"one plain commit","gated":true,"exec_type":"shell","state":"approved",
         "decided_at":"2026-08-26T09:02:00Z","decided_via":"app","thread_id":"t-1a2b3c4d","note":"ok",
         "events":[{"id":1,"action_id":"act-1a2b3c4d","ts":"2026-08-26T09:00:00Z","event":"proposed","actor":"claude:thread:t-1a2b3c4d"},
                   {"id":2,"action_id":"act-1a2b3c4d","ts":"2026-08-26T09:02:00Z","event":"approved","actor":"app","note":"ok"}]}
        """)
        XCTAssertEqual(a.state, "approved")
        XCTAssertEqual(a.events?.count, 2)
        XCTAssertEqual(a.events?.last?.actor, "app")
        XCTAssertNotNil(a.events?.first?.time)
    }

    func testThread() throws {
        let t = try decode(Thread.self, """
        {"id":"t-1a2b3c4d","created_at":"2026-08-26T09:00:00Z","updated_at":"2026-08-26T09:30:00Z","title":"Plan lunches",
         "project":"life","goal_id":"g-health","status":"running","schedule":"daily@08:00","schedule_prompt":"check the fridge",
         "last_run_at":"2026-08-26T09:00:00Z","next_run_at":"2026-08-27T08:00:00-04:00","unread":1,
         "cost_usd":1.25,"cost_by_model":[{"model":"claude-opus-5","cost_usd":1.25}],
         "tokens":12000,"tokens_in":1000,"tokens_out":500,"tokens_cache_read":10000,"tokens_cache_write":500,
         "last_message":"Working on it","last_message_at":"2026-08-26T09:30:00Z","last_message_kind":"message",
         "activity":"reading the fridge list","needs_you":0,"model":"claude-fable-5[1m]",
         "model_label":"fable 5","schedule_label":"daily 08:00","pill":{"word":"running","tone":"running"}}
        """)
        XCTAssertEqual(t.schedule_label, "daily 08:00")
        XCTAssertEqual(t.pill?.tone, "running")
        XCTAssertEqual(t.status, "running")
        XCTAssertEqual(t.cost_usd, 1.25, accuracy: 0.001)
        XCTAssertEqual(t.cost_by_model?.first?.label, "Opus 5")
        XCTAssertEqual(t.modelShort, "fable 5")
        XCTAssertNotNil(t.next_run_at)
    }

    func testCalendarView() throws {
        let v = try decode(CalView.self, """
        {"from":"2026-08-26","to":"2026-10-25","today":"2026-08-26",
         "anytime":[{"id":"ask-1a2b3c4d","day":"","kind":"ask","title":"Pick a day","state":"open","actor":"claude:thread:t-1a2b3c4d","ref":"ask:ask-1a2b3c4d","open":true}],
         "overdue":[],
         "soon":[{"id":"cal-3c4d5e6f","day":"2026-08-20","at":"16:21","kind":"owner","title":"Book the string quartet","state":"scheduled","soon":true,"actor":"claude:thread:t-1a2b3c4d","ref":"cal:cal-3c4d5e6f","lane":"mine","open":true,"item":true,"move":"item","kind_label":"do soon"}],
         "days":[{"day":"2026-08-27","entries":[
           {"id":"cal-1a2b3c4d","day":"2026-08-27","at":"09:00","kind":"owner","title":"Buy the tranche","detail":"5 shares","state":"scheduled","goal_id":"g-money","repeat":"weekly","actor":"owner","ref":"cal:cal-1a2b3c4d","lane":"mine","open":true,"item":true,"move":"item","kind_label":"your step"},
           {"id":"rec-1a2b3c4d","day":"2026-08-27","kind":"rec","title":"Try the standing desk","state":"deferred","actor":"claude:thread:t-1a2b3c4d","ref":"rec:rec-1a2b3c4d","lane":"recs","why":"A recommendation sits at the minute it was filed (dark) or answered (light) — nothing moves it.","kind_label":"check back"},
           {"id":"act-1a2b3c4d","day":"2026-08-27","kind":"action","title":"git commit","state":"proposed","actor":"claude:thread:t-1a2b3c4d","ref":"action:act-1a2b3c4d","lane":"mine","kind_label":"action"},
           {"id":"cal-2b3c4d5e","day":"2026-08-27","kind":"note","title":"Old reminder","state":"dismissed","ref":"cal:cal-2b3c4d5e","lane":"mine","closed":true,"item":true,"why":"Already dismissed — reopen it first.","kind_label":"reminder"},
           {"id":"run:t-1a2b3c4d:1","day":"2026-08-27","at":"08:00","kind":"run","title":"Plan lunches","state":"scheduled","thread_id":"t-1a2b3c4d","actor":"claude:thread:t-1a2b3c4d","ref":"thread:t-1a2b3c4d"}]}]}
        """)
        XCTAssertEqual(v.days.first?.entries.count, 5)
        // A do-soon to-do: no due day, the day it was added, the owner's words close
        // it on the hub when no session is behind it.
        let todo = v.soon?.first
        XCTAssertEqual(todo.map(CalKind.label), "do soon")
        XCTAssertEqual(todo.map(calWhenShort), "added Aug 20")
        XCTAssertTrue(todo?.isLoneTodo ?? false)
        XCTAssertNil(todo?.overdue)
        // The hub's stamps drive lane, closed, drag and the kind's word.
        let step = v.days.first?.entries.first
        XCTAssertEqual(step.map { CalCals.of($0).key }, "mine")
        XCTAssertEqual(step.map(calMovable), .item)
        XCTAssertEqual(step.map(CalKind.label), "your step")
        let note = v.days.first?.entries.first { $0.kind == "note" }
        XCTAssertTrue(note?.isClosed ?? false)
        XCTAssertEqual(note.map(calMovable), CalMove.none)
        XCTAssertEqual(note.map(calWhyStuck), "Already dismissed — reopen it first.")
        let rec = v.days.first?.entries.first { $0.kind == "rec" }
        XCTAssertEqual(rec.map { CalCals.of($0).key }, "recs")
        XCTAssertEqual(rec?.refKind, "rec")
        XCTAssertEqual(rec?.refID, "rec-1a2b3c4d")
        XCTAssertEqual(v.days.first?.entries.first?.`repeat`, "weekly")
        XCTAssertTrue(v.days.first?.entries.first?.isItem ?? false)
        // A proposed action and a deferred rec are neither the owner's to swipe
        // (isOpen) nor finished (isClosed): the agenda must not draw either
        // with the done check.
        let act = v.days.first?.entries.first { $0.kind == "action" }
        XCTAssertFalse(act?.isOpen ?? true)
        XCTAssertFalse(act?.isClosed ?? true)
        XCTAssertFalse(rec?.isClosed ?? true)
        XCTAssertTrue(v.days.first?.entries.first?.isOpen ?? false)
    }

    func testRec() throws {
        let r = try decode(Rec.self, """
        {"id":"rec-1a2b3c4d","created_at":"2026-08-26T09:00:00Z","updated_at":"2026-08-26T09:00:00Z","title":"Try the standing desk",
         "detail":"Because…","goal_id":"g-health","thread_id":"t-1a2b3c4d","source":"claude:thread:t-1a2b3c4d","domain":"health","kind":"buy",
         "cost_cents":29900,"cost_period":"","effort":"low","confidence":70,"because":"you sit all day","expect":"less back pain",
         "act_by":"2026-09-30","review_on":"2026-09-26","status":"proposed","open":true,"outcome":"","model":"claude-fable-5[1m]",
         "cost_label":"$299 one-off","days_left":16,"dates_label":"act by Sep 30 (16d) · review Sep 26"}
        """)
        XCTAssertEqual(r.costLabel, "$299 one-off")
        XCTAssertEqual(r.days_left, 16)
        XCTAssertEqual(recDates(r), "act by Sep 30 (16d) · review Sep 26")
        XCTAssertEqual(r.modelShort, "fable 5")
        XCTAssertTrue(r.isOpen)
    }

    /// Go's zero time is "at once", not a date in year 1.
    func testPromptZeroNotBefore() throws {
        let p = try decode(Prompt.self, """
        {"id":"p-1a2b3c4d","created_at":"2026-08-26T09:00:00Z","author":"owner","target":"t-1a2b3c4d",
         "not_before":"0001-01-01T00:00:00Z","text":"go on","state":"queued"}
        """)
        XCTAssertNil(p.not_before)
        XCTAssertEqual(p.whenLabel, "now")
        let later = try decode(Prompt.self, """
        {"id":"p-2","created_at":"2026-08-26T09:00:00Z","author":"owner","target":"new","not_before":"2026-08-27T09:00:00Z","text":"x","state":"queued"}
        """)
        XCTAssertNotNil(later.not_before)
        XCTAssertTrue(later.startsNewSession)
    }

    func testModelSettingMissingKeys() throws {
        let m = try decode(ModelSetting.self, #"{"default_model":"claude-opus-5"}"#)
        XCTAssertEqual(m.default_model, "claude-opus-5")
        XCTAssertFalse(m.explicit)
        XCTAssertEqual(m.options, [])
    }

    /// The "will run:" block: a string prints as itself, an object as JSON.
    func testActionExecPayload() throws {
        let base = #""id":"act-1","created_at":"2026-08-26T09:00:00Z","updated_at":"2026-08-26T09:00:00Z","project":"life","kind":"commit","title":"t","detail":"","gated":true,"exec_type":"shell","state":"proposed""#
        let s = try decode(Action.self, "{\(base),\"exec_payload\":\"git commit -m x\"}")
        XCTAssertEqual(s.exec_payload?.text, "git commit -m x")
        let o = try decode(Action.self, "{\(base),\"exec_payload\":{\"cmd\":\"make\",\"args\":[\"check\"],\"n\":2}}")
        XCTAssertEqual(o.exec_payload?.text.contains("\"cmd\" : \"make\""), true)
        XCTAssertEqual(o.exec_payload?.text.contains("\"n\" : 2"), true)
        let none = try decode(Action.self, "{\(base),\"exec_payload\":null}")
        XCTAssertNil(none.exec_payload)
    }
}

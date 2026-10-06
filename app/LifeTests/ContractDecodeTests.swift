// The rest of the contract: one decode test per hub response type the app
// reads that DecodeTests.swift did not already cover.
// Every fixture is written by hand from shared/api.md with invented values —
// never captured from data/ — and where the contract marks a field `?` there
// is a fixture with it present and one without, so a hub that stops sending
// it (or a model that stops tolerating its absence) fails here, not on the
// phone.
import XCTest
@testable import Life

final class ContractDecodeTests: XCTestCase {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try HubClient.decoder().decode(T.self, from: Data(json.utf8))
    }

    // MARK: GET /projects · /spend/summary · /spend/quota

    func testProjects() throws {
        let p = try decode([Project].self, #"[{"name":"life","dir":"/Users/x/life"},{"name":"site","dir":"/Users/x/life/projects/site"}]"#)
        XCTAssertEqual(p.count, 2)
        XCTAssertEqual(p.first?.id, "life")
    }

    func testSpendSummary() throws {
        let s = try decode(SpendSummary.self, """
        {"generated_at":"2026-08-26T09:00:00Z","days":0,"window_hours":5,"total_usd":12.5,"today_usd":3.25,"messages":40,"unknown_models":["claude-x"],
         "by_day":[{"key":"2026-08-26","usd":3.25,"messages":10,"input_tokens":1000,"output_tokens":500,"cache_read_tokens":20000,"cache_write_tokens":300,
                    "models":[{"key":"claude-opus-5","usd":3.25,"messages":10,"input_tokens":1000,"output_tokens":500,"cache_read_tokens":20000,"cache_write_tokens":300}]}],
         "history":[{"key":"2026-08-20","usd":9.25,"messages":30,"input_tokens":3000,"output_tokens":1500,"cache_read_tokens":60000,"cache_write_tokens":900,
                    "models":[{"key":"claude-opus-5","usd":9.25,"messages":30,"input_tokens":3000,"output_tokens":1500,"cache_read_tokens":60000,"cache_write_tokens":900}]},
                    {"key":"2026-08-26","usd":3.25,"messages":10,"input_tokens":1000,"output_tokens":500,"cache_read_tokens":20000,"cache_write_tokens":300,
                    "models":[{"key":"claude-opus-5","usd":3.25,"messages":10,"input_tokens":1000,"output_tokens":500,"cache_read_tokens":20000,"cache_write_tokens":300}]}],
         "by_project":[{"key":"life","usd":12.5,"messages":40,"input_tokens":4000,"output_tokens":2000,"cache_read_tokens":80000,"cache_write_tokens":1200}],
         "by_model":[{"key":"claude-opus-5","usd":12.5,"messages":40,"input_tokens":4000,"output_tokens":2000,"cache_read_tokens":80000,"cache_write_tokens":1200}],
         "palette":["claude-opus-5","","","","","","",""],
         "sessions":[{"session_id":"s-1","project":"life","cwd":"/Users/x/life","start":"2026-08-26T08:00:00Z","end":"2026-08-26T09:00:00Z","usd":12.5,"messages":40,"models":["claude-opus-5"]}]}
        """)
        XCTAssertEqual(s.window_hours, 5)
        XCTAssertEqual(s.by_day.first?.id, "2026-08-26")
        XCTAssertEqual(s.by_day.first?.models?.first?.key, "claude-opus-5")
        XCTAssertEqual(s.history.first?.id, "2026-08-20")   // all-time, oldest first, whatever the window
        XCTAssertEqual(s.history.count, 2)
        XCTAssertNil(s.by_model.first?.models)
        XCTAssertEqual(s.palette.firstIndex(of: "claude-opus-5"), 0)
        XCTAssertEqual(s.sessions.first?.models, ["claude-opus-5"])
        XCTAssertEqual(s.by_model.first?.cache_read_tokens, 80000)
    }

    func testQuotaFull() throws {
        let q = try decode(Quota.self, """
        {"generated_at":"2026-08-26T09:00:00Z","fetched_at":"2026-08-26T08:59:30Z","available":true,
         "windows":[{"key":"five_hour","label":"All models · 5 hours","note":"every model's spend","utilization":42.5,
                     "resets_at":"2026-08-26T12:00:00Z","starts_at":"2026-08-26T07:00:00Z","spent_usd":4.2,"messages":12,"headroom_usd":5.68,
                     "by_model":[{"key":"claude-opus-5","usd":4.2,"messages":12,"input_tokens":1,"output_tokens":1,"cache_read_tokens":1,"cache_write_tokens":1}],
                     "burn_pct_per_hour":10.5,"full_at":"2026-08-26T14:30:00Z",
                     "measured_delta_pct":8,"measured_span_hours":2,"measured_pct_per_hour":4,
                     "typical_pct_per_hour":6,"full_at_typical":"2026-08-26T18:00:00Z"},
                    {"key":"seven_day_fable","label":"Fable · 7 days","note":"Fable only","utilization":3,"scope_model":"fable",
                     "resets_at":"0001-01-01T00:00:00Z","spent_usd":0,"messages":0,"headroom_usd":0,"by_model":[],"burn_pct_per_hour":0,"full_at":"0001-01-01T00:00:00Z"}]}
        """)
        XCTAssertTrue(q.available)
        XCTAssertNil(q.error)
        let w = try XCTUnwrap(q.windows.first)
        XCTAssertEqual(w.burn, 10.5)
        XCTAssertNotNil(w.fills)
        XCTAssertEqual(w.measuredHours, 2)
        // Go's zero time is "unknown", not the year 1.
        let f = try XCTUnwrap(q.windows.last)
        XCTAssertEqual(f.scope_model, "fable")
        XCTAssertNil(f.resets)
        XCTAssertNil(f.fills)
    }

    func testQuotaUnavailable() throws {
        let q = try decode(Quota.self, #"{"generated_at":"2026-08-26T09:00:00Z","available":false,"error":"no Keychain item","windows":[]}"#)
        XCTAssertFalse(q.available)
        XCTAssertEqual(q.error, "no Keychain item")
        XCTAssertNil(q.fetched_at)
    }

    // MARK: GET /changes

    func testChangeFeed() throws {
        let f = try decode(ChangeFeed.self, #"{"version":"9a1b2c3d4e5f6071","changed":true}"#)
        XCTAssertEqual(f.version, "9a1b2c3d4e5f6071")
        XCTAssertTrue(f.changed)
    }

    func testJobRun() throws {
        let r = try decode(JobRun.self, """
        {"id":17,"job":"spend-watch","started_at":"2026-08-26T09:00:00Z","finished_at":"2026-08-26T09:03:00Z","ok":true,"summary":"nothing unusual",
         "cost_usd":0.4,"session_id":"s-1","text":"I looked at the last week.",
         "findings":[{"kind":"other","title":"Two duplicate subscriptions","detail":"both bill monthly","exec_type":"none","exec_payload":{}}],
         "needs_you":["Cancel one of them"]}
        """)
        XCTAssertEqual(r.id, 17)
        XCTAssertEqual(r.findings.first?.title, "Two duplicate subscriptions")
        XCTAssertEqual(r.needs_you, ["Cancel one of them"])
        // A run that did not parse: everything optional absent, lists empty.
        let bare = try decode(JobRun.self, #"{"id":18,"job":"spend-watch","started_at":"2026-08-26T09:00:00Z","cost_usd":0,"findings":[],"needs_you":[]}"#)
        XCTAssertNil(bare.ok)
        XCTAssertNil(bare.text)
    }

    // MARK: GET /status · /goals · /goals/{id}/notes

    func testHubStatus() throws {
        let s = try decode(HubStatus.self, """
        {"ok":true,"time":"2026-08-26T09:00:00Z","uptime_s":3600,"pending_actions":2,"jobs":3,
         "app_installed":"2026-08-25","app_profile_expires":"2027-08-23","app_profile_days_left":362,
         "ota":{"version":"1.0.405","build":405,"url":"https://hub.example/ota/tok/install.html","profile_expires":"2027-08-23","aps":"production","built_at":"2026-08-26T03:00:00Z","commit":"abc1234","head":407,"app_changed":true},
         "devices":[{"token":"ab12cd","env":"production","build":405}]}
        """)
        XCTAssertEqual(s.pending_actions, 2)
        XCTAssertEqual(s.ota?.build, 405)
        XCTAssertEqual(s.ota?.app_changed, true)
        XCTAssertEqual(s.ota?.installURL?.scheme, "itms-services")
        XCTAssertEqual(s.devices?.first?.env, "production")
        let bare = try decode(HubStatus.self, #"{"ok":true,"time":"2026-08-26T09:00:00Z","uptime_s":1,"pending_actions":0,"jobs":0}"#)
        XCTAssertNil(bare.ota)
        XCTAssertNil(bare.app_profile_days_left)
    }

    func testGoal() throws {
        let g = try decode(Goal.self, """
        {"id":"make-me-healthier","created_at":"2026-08-21T02:00:00Z","updated_at":"2026-08-21T02:00:00Z","title":"Make me healthier","statement":"Sleep, activity, nutrition.",
         "horizon":"ongoing","cadence":"weekly","status":"active","sources":"health,photos","last_reviewed_at":"2026-08-24T02:33:52Z","note_count":5,
         "emblem":{"symbol":"health","hue":350}}
        """)
        XCTAssertEqual(g.cadence, "weekly")
        XCTAssertNotNil(g.last_reviewed_at)
        XCTAssertEqual(g.emblem.systemImage, "heart.fill")
        let never = try decode(Goal.self, #"{"id":"g","created_at":"2026-08-21T02:00:00Z","updated_at":"2026-08-21T02:00:00Z","title":"G","statement":"","horizon":"year","cadence":"monthly","status":"paused","sources":"","note_count":0,"emblem":{"symbol":"goal","hue":123}}"#)
        XCTAssertNil(never.last_reviewed_at)
        XCTAssertEqual(never.emblem.systemImage, "target")
    }

    func testGoalNotes() throws {
        let n = try decode([GoalNote].self, #"[{"id":9,"goal_id":"make-me-healthier","created_at":"2026-08-26T09:00:00Z","author":"claude:thread:t-1a2b3c4d","kind":"evidence","text":"Walked 9,000 steps."}]"#)
        XCTAssertEqual(n.first?.id, 9)
        XCTAssertEqual(n.first?.kind, "evidence")
    }

    // MARK: Sources

    func testObservationPayload() throws {
        let o = try decode([Obs].self, """
        [{"id":501,"source":"app","kind":"photo","ts":"2026-08-26T09:00:00Z","tz":"America/New_York",
          "payload":{"via":"session","thread_id":"t-1a2b3c4d","blob_bytes":12345,"ok":true,"tags":["fridge","milk"],"nested":{"k":null}},
          "blob_ref":"sha256/ab/abcd.jpg","ingested_at":"2026-08-26T09:00:01Z","schema_version":1}]
        """)
        let p = try XCTUnwrap(o.first?.payload)
        XCTAssertEqual(p["via"]?.string, "session")
        XCTAssertEqual(p["blob_bytes"]?.number, 12345)
        XCTAssertEqual(p["ok"]?.bool, true)
        if case .array(let a) = try XCTUnwrap(p["tags"]) { XCTAssertEqual(a.count, 2) } else { XCTFail("tags") }
        if case .object(let n) = try XCTUnwrap(p["nested"]) { XCTAssertEqual(n["k"], .null) } else { XCTFail("nested") }
        XCTAssertEqual(o.first?.blob_ref, "sha256/ab/abcd.jpg")
    }

    func testSourcesInventory() throws {
        let s = try decode(SourcesInventory.self, """
        {"groups":[{"id":"body","title":"Body","blurb":"What the phone measures",
                    "sources":[{"id":"health","title":"Apple Health","from":"the phone","storage":"observations","status":"live","last":"2026-08-26T08:00:00Z","total":3,
                                "kinds":[{"kind":"steps","n":3,"first":"2026-08-24T00:00:00Z","last":"2026-08-26T00:00:00Z","note":"one row per day"}]},
                               {"id":"polar","title":"Polar","from":"not connected","storage":"—","status":"planned","total":0,"kinds":[]}]}]}
        """)
        XCTAssertEqual(s.groups.first?.sources.count, 2)
        XCTAssertEqual(s.groups.first?.sources.first?.kinds.first?.note, "one row per day")
        XCTAssertNil(s.groups.first?.sources.last?.last)
    }

    // MARK: Threads — messages and events

    func testThreadMessage() throws {
        let m = try decode(ThreadMessage.self, """
        {"id":42,"thread_id":"t-1a2b3c4d","ts":"2026-08-26T09:00:00Z","role":"owner","kind":"message","text":"Plan lunches",
         "cost_usd":0,"tokens":0,"tokens_in":0,"tokens_out":0,"tokens_cache_read":0,"tokens_cache_write":0,
         "attachments":["sha256/ab/abcd.jpg"],"run_id":"r1","queued":false,"steered":true}
        """)
        XCTAssertEqual(m.attachments?.count, 1)
        XCTAssertEqual(m.steered, true)
        let old = try decode(ThreadMessage.self, #"{"id":1,"thread_id":"t-1a2b3c4d","ts":"2026-08-21T09:00:00Z","role":"claude","kind":"needs_you","text":"Done.","cost_usd":0.5}"#)
        XCTAssertNil(old.tokens)
        XCTAssertNil(old.attachments)
    }

    func testThreadEvents() throws {
        let e = try decode([ThreadEvent].self, """
        [{"id":7,"thread_id":"t-1a2b3c4d","run_id":"r1","ts":"2026-08-26T09:00:00Z","kind":"tool_use","title":"Bash · lifectl goals","body":"lifectl goals","summary":"List goals"},
         {"id":8,"thread_id":"t-1a2b3c4d","run_id":"r1","ts":"2026-08-26T09:00:01Z","kind":"tool_result","title":"result","body":"[...]"}]
        """)
        XCTAssertEqual(e.first?.summary, "List goals")
        XCTAssertNil(e.last?.summary)
    }

    // MARK: Calendar item · engagement · recs

    func testCalItem() throws {
        let i = try decode(CalItem.self, """
        {"id":"cal-1a2b3c4d","created_at":"2026-08-26T09:00:00Z","updated_at":"2026-08-26T09:00:00Z","title":"Buy the tranche","detail":"5 shares","kind":"owner",
         "day":"2026-08-27","at":"09:00","repeat":"weekly","goal_id":"g-money","thread_id":"t-1a2b3c4d","source":"claude:thread:t-1a2b3c4d","state":"fired",
         "nag_min":360,"nag_count":1,"last_nag_at":"2026-08-27T15:00:00Z","fired_at":"2026-08-27T13:00:00Z","ask_id":"ask-1a2b3c4d","ask_kind":"physical","check_hint":"a buy in the trade log","prev_id":"cal-00000001"}
        """)
        XCTAssertEqual(i.`repeat`, "weekly")
        XCTAssertEqual(i.ask_id, "ask-1a2b3c4d")
        XCTAssertNil(i.resolved_at)
        let note = try decode(CalItem.self, #"{"id":"cal-2","created_at":"2026-08-26T09:00:00Z","updated_at":"2026-08-26T09:00:00Z","title":"Vest","detail":"","kind":"note","day":"2026-11-01","at":"","repeat":"","source":"owner","state":"scheduled","nag_min":360,"nag_count":0}"#)
        XCTAssertNil(note.ask_id)
        XCTAssertEqual(note.at, "")
    }

    func testRecsPageAndDeferred() throws {
        let page = try decode(RecsPage.self, """
        {"recs":[{"id":"rec-1a2b3c4d","created_at":"2026-08-26T09:00:00Z","updated_at":"2026-08-26T09:00:00Z","title":"Move the cash","source":"owner","domain":"money","kind":"trade",
                  "cost_cents":0,"effort":"low","confidence":50,"status":"deferred","review_on":"2026-09-01","decided_at":"2026-08-26T10:00:00Z","decided_by":"owner","decision_note":"after payday","outcome":"","cost_label":"free","dates_label":"back Sep 1"}]}
        """)
        let r = try XCTUnwrap(page.recs.first)
        // Deferred: neither open nor closed (store.RecStanding) — still
        // decidable, not on the open list.
        XCTAssertFalse(r.isClosed)
        XCTAssertFalse(r.isOpen)
        XCTAssertNil(r.model)
        XCTAssertNil(r.modelShort)
        XCTAssertEqual(r.costLabel, "free")
    }

    func testRecStarter() throws {
        let s = try decode(RecStarter.self, #"{"rec_id":"rec-1a2b3c4d","goal_id":"g-health","opener":"Accepted: Try the standing desk","context":"- rec rec-1a2b3c4d\n- $299","relay":"Alex accepted.","source_session":"t-1a2b3c4d"}"#)
        XCTAssertEqual(s.source_session, "t-1a2b3c4d")
        let orphan = try decode(RecStarter.self, #"{"rec_id":"rec-2","opener":"o","context":"c","relay":"r","source_session":""}"#)
        XCTAssertNil(orphan.goal_id)
    }

    func testRecDecision() throws {
        let d = try decode(RecDecision.self, """
        {"id":"rec-1a2b3c4d","created_at":"2026-08-26T09:00:00Z","updated_at":"2026-08-26T10:00:00Z","title":"Try the standing desk","source":"claude:thread:t-1a2b3c4d","domain":"health","kind":"buy",
         "cost_cents":29900,"effort":"low","confidence":70,"status":"accepted","review_on":"2026-09-25","decided_at":"2026-08-26T10:00:00Z","decided_by":"owner","outcome":"","model":"claude-opus-5",
         "delivered":"new","session_id":"t-9f8e7d6c"}
        """)
        XCTAssertEqual(d.rec.status, "accepted")
        XCTAssertEqual(d.delivered, "new")
        XCTAssertEqual(d.session_id, "t-9f8e7d6c")
        XCTAssertNil(d.delivery_error)
    }

    // MARK: Errors · app install status

    func testAPIError() throws {
        let e = try decode(APIError.self, #"{"error":"unknown thread"}"#)
        XCTAssertEqual(e.errorDescription, "unknown thread")
    }

    func testInstallStatus() throws {
        let s = try decode(HubClient.InstallStatus.self, #"{"running":false,"tail":["archive ok","published 405"],"log_at":"2026-08-26T03:00:00Z","ota":{"version":"1.0.405","build":405,"url":"https://hub.example/ota/tok/install.html","head":407,"app_changed":false}}"#)
        XCTAssertEqual(s.tail.count, 2)
        XCTAssertEqual(s.ota?.head, 407)
        let live = try decode(HubClient.InstallStatus.self, #"{"running":true,"tail":[]}"#)
        XCTAssertNil(live.ota)
        XCTAssertNil(live.log_at)
    }
}

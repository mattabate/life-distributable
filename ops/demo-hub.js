#!/usr/bin/env node
// A stand-in hub for the README's phone screenshots: serves the demo world
// in ops/demo-fixtures.js (the maker's own hub, scrubbed) on
// http://127.0.0.1:<port>/api/v1/… so the real iPhone app in the Simulator
// can be photographed showing it. Reads nothing from any real hub; every write answers
// {ok:true} and changes nothing. Routes the demo lacks fall back to
// shared/fixtures/<path_with_underscores>.json; anything else is [].
//
// Usage: node ops/demo-hub.js [port]   (default 8787); ops/demo-screens.sh runs it.
//
// The web console is lenient about missing fields; the app's Codable models
// are not (a Thread needs created_at, project, schedule…), so each kind of
// object is completed with the hub's defaults before it goes out.
'use strict';
const http = require('http');
const fs = require('fs');
const path = require('path');
const vm = require('vm');

const ROOT = path.resolve(__dirname, '..');
const PORT = Number(process.argv[2] || 8787);

// demo-fixtures.js is an IIFE that sets window.DEMO; give it a window.
const sandbox = { window: {}, URLSearchParams, Date, console };
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync(path.join(__dirname, 'demo-fixtures.js'), 'utf8'), sandbox);
const DEMO = sandbox.window.DEMO;

const now = () => new Date().toISOString();
const ago = m => new Date(Date.now() - m * 60000).toISOString();

function shared(clean) {
  const f = path.join(ROOT, 'shared/fixtures', clean.replace(/^\//, '').split('/').join('_') + '.json');
  try { return JSON.parse(fs.readFileSync(f, 'utf8')); } catch (e) { return undefined; }
}

// Completion per object kind: only what the app's models require.
const fill = {
  thread: t => Object.assign({ created_at: ago(3000), updated_at: t.last_message_at || now(), project: 'life', schedule: '',
    schedule_prompt: '', unread: 0, cost_usd: 0, needs_you: 0 }, t),
  ask: a => Object.assign({ created_at: ago(60), updated_at: a.created_at || ago(60), detail: '', check_hint: '', state: 'active',
    thread_id: '', kind: 'other' }, a),
  action: a => Object.assign({ created_at: ago(60), updated_at: a.created_at || ago(60), project: 'life', detail: '', gated: true,
    exec_type: 'claude', state: 'proposed', open: (a.state || 'proposed') === 'proposed', lane: 'mine' }, a, { exec_payload: a.exec_payload && typeof a.exec_payload !== 'string'
      ? JSON.stringify(a.exec_payload, null, 2) : a.exec_payload }),
  goal: g => Object.assign({ statement: '', horizon: 'year', cadence: 'weekly', status: 'active', sources: '', note_count: 0,
    emblem: { symbol: 'goal', hue: 200 } }, g),
  note: n => Object.assign({ kind: 'note', author: 'owner' }, n, { by: n.by || (n.author === 'owner' ? 'you' : n.author) }),
  rec: r => Object.assign({ created_at: ago(600), updated_at: r.created_at || ago(600), source: r.thread_id ? 'claude:thread:' + r.thread_id : 'owner',
    domain: 'other', kind: 'try', cost_cents: 0, effort: 'low', confidence: 50, status: 'proposed', lane: 'mine', window: 'soon' }, r),
  message: (m, tid) => Object.assign({ thread_id: tid, kind: m.role === 'owner' ? 'owner' : 'reply', cost_usd: 0 }, m),
};

function complete(p, body) {
  if (body === undefined || body === null) return body;
  if (p === '/threads') return body.map(fill.thread);
  if (/^\/threads\/[^/]+$/.test(p)) return fill.thread(body);
  if (/^\/threads\/[^/]+\/messages$/.test(p)) return body.map(m => fill.message(m, p.split('/')[2]));
  if (p === '/asks' || /^\/asks\/[^/]+$/.test(p)) return Array.isArray(body) ? body.map(fill.ask) : fill.ask(body);
  if (p === '/actions' || /^\/actions\/[^/]+$/.test(p)) return Array.isArray(body) ? body.map(fill.action) : fill.action(body);
  if (p === '/recs') return Object.assign({}, body, { recs: (body.recs || []).map(fill.rec) });
  if (/^\/recs\/[^/]+$/.test(p) && body.id) return fill.rec(body);
  if (p === '/goals') return body.map(fill.goal);
  if (/^\/goals\/[^/]+$/.test(p)) return fill.goal(body);
  if (/^\/goals\/[^/]+\/notes$/.test(p)) return body.map(fill.note);
  if (p === '/board') {
    const b = Object.assign({}, body, { surface: 'mobile' });
    b.headings = (b.headings || []).map(h => Object.assign({ show: 0 }, h));
    b.sessions = (b.sessions || []).map(s => Object.assign({}, s, { actions: (s.actions || []).map(fill.action), asks: (s.asks || []).map(fill.ask), steps: (s.steps || []).map(fill.ask) }));
    return b;
  }
  if (p === '/status') return Object.assign({ ok: true, uptime_s: 86400 * 3, pending_actions: 2, jobs: 1 }, body);
  if (p === '/spend/quota' && body.windows) {
    body.windows = body.windows.map(w => Object.assign({ spent_usd: 0, messages: 0, headroom_usd: 0, by_model: [] }, w));
  }
  return body;
}

function answer(fullPath) {
  const [p, q] = fullPath.split('?');
  if (p === '/changes') return { version: 'demo', changed: false };
  if (p === '/threads' && new URLSearchParams(q || '').get('archived')) return [];
  if (/^\/threads\/[^/]+\/(events|steps)$/.test(p)) return [];
  if (p === '/usage') return { on: false };
  let body = DEMO(fullPath);
  if (body === undefined) body = shared(p);
  if (body === undefined) body = [];
  return complete(p, body);
}

http.createServer((req, res) => {
  const url = req.url.replace(/^.*\/api\/v1/, '');
  let body;
  if (req.method !== 'GET') body = { ok: true };
  else if (url.startsWith('/changes')) {
    // The app parks here for `wait` seconds; hold it a little so it does not spin.
    setTimeout(() => { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ version: 'demo', changed: false })); }, 20000);
    return;
  } else body = answer(url);
  res.writeHead(200, { 'Content-Type': 'application/json', ETag: '"demo-' + Date.now() + '"' });
  res.end(JSON.stringify(body));
}).listen(PORT, '127.0.0.1', () => console.log(`demo hub on http://127.0.0.1:${PORT}`));

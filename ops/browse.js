#!/usr/bin/env node
// Drive the laptop console in headless Chrome — click, type, hover, scroll,
// screenshot — so a session can WALK a flow instead of looking at one frame.
//
// `ops/web-smoke.sh shot` renders a route and stops; anything behind a click
// (an account page, the Respond chips, a menu, the composer mid-type) could
// not be looked at by anything but the owner. This talks the Chrome DevTools
// Protocol, so the page is a live one being used.
//
// No dependencies on purpose: node has no package.json here and a `npm
// install` is not something a session may do, so the ~90 lines under "wire"
// are a minimal RFC6455 client. CDP is plain ws on 127.0.0.1.
//
// Usage:
//   ops/browse.js "<step>; <step>; …"          steps inline
//   ops/browse.js --file ops/flows/money.txt   one step per line (# comments)
//   ops/browse.js --out <dir> --strict --keep --size 1500x1000 --scale 2 --dark …
//   ops/browse.js --web <worktree>/hub/internal/server/web …   an edited console, live data
//
// Steps (verb + argument):
//   goto <#route|url>     #route resolves against the hub URL, token included
//   click <sel>           real mouse press/release at the element's centre
//   hover <sel>           mouse move (menus, tooltips, :hover styling)
//   fill <sel> <text>     focus, select all, type (fires input/change)
//   press <Enter|Escape|Tab|Space|Backspace|Delete|Arrow*|a single letter>
//   scroll <px|bottom|top|sel>   assigns scrollTop on the page's own scroller
//   wheel <±px> [sel]     a REAL wheel over that element (or the centre)
//   wait <ms|sel>         a bare number is milliseconds, else wait for <sel>
//   shot <name>           PNG (full page) → <out>/NN-<name>.png
//   shotv <name>          PNG (the viewport only, hover state intact)
//   dom <name>            outerHTML   → <out>/NN-<name>.html
//   text <sel>            print the element's innerText (visible text only)
//   eval <js>             print the expression's value
//
// <sel> is a CSS selector, or `text=Some words` to match visible text (the
// smallest element containing it), which is how a button with no id is
// reachable.
//
// Every step prints one line. Console errors and uncaught exceptions from the
// page are collected and printed at the end — a JS error that only shows in
// devtools is exactly the class of bug this exists to find. --strict exits 1
// if any were seen.
'use strict';
const net = require('net');
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const { spawn, execFileSync } = require('child_process');
const os = require('os');

const ROOT = path.resolve(__dirname, '..');
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

// ---------------------------------------------------------------- arguments
const argv = process.argv.slice(2);
let stepsText = '';
let out = path.join(ROOT, 'ops/logs/web/browse');
let size = '1500x1000';
let scale = 1; // --scale 2: retina pixels, for a picture that goes in a README
let strict = false;
let keep = false; // keep the user-data-dir (cookies/localStorage) between runs
let base = '';
let dark = false; // --dark: the page under prefers-color-scheme: dark (a picture of the screen after sunset)
// --web <dir>: serve index.html and /static/* from this directory instead of
// the hub's embedded copy, so an edited console (a worktree's web/) can be
// looked at against live data without rebuilding or restarting the hub.
let web = '';
for (let i = 0; i < argv.length; i++) {
  const a = argv[i];
  if (a === '--dark') dark = true;
  else if (a === '--file') stepsText += fs.readFileSync(argv[++i], 'utf8') + '\n';
  else if (a === '--out') out = path.resolve(argv[++i]);
  else if (a === '--size') size = argv[++i];
  else if (a === '--scale') scale = Number(argv[++i]) || 1;
  else if (a === '--url') base = argv[++i];
  else if (a === '--strict') strict = true;
  else if (a === '--keep') keep = true;
  else if (a === '--web') web = path.resolve(argv[++i]);
  else stepsText += a + '\n';
}
// `;` separates steps in the one-liner form (STEPS='goto #/money; shot money').
// A line is a step on its own, so the comment goes FIRST — a semicolon inside a
// sentence in a flow file used to become a step called "this" — and an `eval`
// line is handed over whole: everything after the verb is JavaScript, where `;`
// is a statement separator and not ours to take (2026-09-01, writing the
// time-axis walk, whose assertions need more than one statement).
const steps = stepsText
  .split('\n')
  .map((l) => l.trim())
  .filter((l) => l && !l.startsWith('#'))
  .flatMap((l) => (/^eval\s/.test(l) ? [l] : l.split(';')))
  .map((l) => l.trim())
  .filter(Boolean);
if (!steps.length) {
  console.error('browse: no steps. Example:\n  ops/browse.js "goto #/money; shot money; click text=Checking; shot account"');
  process.exit(2);
}
if (!fs.existsSync(CHROME)) { console.error('browse: no Chrome at ' + CHROME); process.exit(2); }

// The hub URL carries the token; `goto #/money` must land authenticated.
// A git worktree has no secrets/: the token is the main checkout's.
let hubRoot = ROOT;
if (!fs.existsSync(path.join(ROOT, 'secrets/hub.token'))) {
  try { hubRoot = path.dirname(execFileSync('git', ['-C', ROOT, 'rev-parse', '--path-format=absolute', '--git-common-dir']).toString().trim()); } catch {}
}
if (!base) base = execFileSync('bash', [path.join(hubRoot, 'ops/hub.sh'), 'url'], { cwd: hubRoot }).toString().trim();
const hubBase = base.replace(/#.*$/, '');
const [winW, winH] = size.split('x').map(Number);

// -------------------------------------------------------------------- wire
// A WebSocket client small enough to read: CDP frames are text, the server
// never masks, and the only control frames that matter are ping and close.
class WS {
  constructor(url, onMessage) {
    const u = new URL(url);
    this.onMessage = onMessage;
    this.buf = Buffer.alloc(0);
    this.frag = [];
    this.open = false;
    this.queue = [];
    const key = crypto.randomBytes(16).toString('base64');
    this.sock = net.connect(Number(u.port), u.hostname, () => {
      this.sock.write(
        `GET ${u.pathname}${u.search} HTTP/1.1\r\nHost: ${u.host}\r\n` +
        `Upgrade: websocket\r\nConnection: Upgrade\r\n` +
        `Sec-WebSocket-Key: ${key}\r\nSec-WebSocket-Version: 13\r\n\r\n`
      );
    });
    this.sock.on('data', (d) => this.feed(d));
    this.sock.on('error', (e) => { throw e; });
  }
  feed(d) {
    this.buf = Buffer.concat([this.buf, d]);
    if (!this.open) {
      const end = this.buf.indexOf('\r\n\r\n');
      if (end < 0) return;
      const head = this.buf.slice(0, end).toString();
      if (!/101/.test(head.split('\r\n')[0])) throw new Error('websocket upgrade refused: ' + head.split('\r\n')[0]);
      this.buf = this.buf.slice(end + 4);
      this.open = true;
      for (const m of this.queue) this.send(m);
      this.queue = [];
    }
    for (;;) {
      const f = this.readFrame();
      if (!f) return;
      if (f.opcode === 0x9) { this.frame(0xa, f.payload); continue; } // ping → pong
      if (f.opcode === 0x8) { this.sock.end(); return; }
      if (f.opcode === 0x0 || f.opcode === 0x1) {
        this.frag.push(f.payload);
        if (f.fin) { const s = Buffer.concat(this.frag).toString('utf8'); this.frag = []; this.onMessage(s); }
      }
    }
  }
  readFrame() {
    const b = this.buf;
    if (b.length < 2) return null;
    const fin = (b[0] & 0x80) !== 0, opcode = b[0] & 0x0f;
    let len = b[1] & 0x7f, off = 2;
    if (len === 126) { if (b.length < 4) return null; len = b.readUInt16BE(2); off = 4; }
    else if (len === 127) { if (b.length < 10) return null; len = Number(b.readBigUInt64BE(2)); off = 10; }
    if (b.length < off + len) return null; // screenshots arrive in many packets
    const payload = b.slice(off, off + len);
    this.buf = b.slice(off + len);
    return { fin, opcode, payload };
  }
  frame(opcode, payload) {
    const mask = crypto.randomBytes(4);
    const data = Buffer.from(payload);
    const masked = Buffer.alloc(data.length);
    for (let i = 0; i < data.length; i++) masked[i] = data[i] ^ mask[i % 4];
    let head;
    if (data.length < 126) head = Buffer.from([0x80 | opcode, 0x80 | data.length]);
    else if (data.length < 65536) { head = Buffer.alloc(4); head[0] = 0x80 | opcode; head[1] = 0x80 | 126; head.writeUInt16BE(data.length, 2); }
    else { head = Buffer.alloc(10); head[0] = 0x80 | opcode; head[1] = 0x80 | 127; head.writeBigUInt64BE(BigInt(data.length), 2); }
    this.sock.write(Buffer.concat([head, mask, masked]));
  }
  send(s) { if (!this.open) this.queue.push(s); else this.frame(0x1, s); }
  close() { try { this.frame(0x8, Buffer.alloc(0)); this.sock.end(); } catch {} }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function getJSON(url, tries = 60) {
  for (let i = 0; i < tries; i++) {
    try {
      const r = await fetch(url);
      if (r.ok) return await r.json();
    } catch {}
    await sleep(250);
  }
  throw new Error('chrome did not answer ' + url);
}

// --------------------------------------------------------------------- CDP
class CDP {
  constructor(ws) { this.ws = ws; this.id = 0; this.waiting = new Map(); this.handlers = []; }
  message(raw) {
    const m = JSON.parse(raw);
    if (m.id && this.waiting.has(m.id)) {
      const { resolve, reject } = this.waiting.get(m.id);
      this.waiting.delete(m.id);
      m.error ? reject(new Error(m.error.message)) : resolve(m.result);
    } else if (m.method) for (const h of this.handlers) h(m);
  }
  on(fn) { this.handlers.push(fn); }
  send(method, params = {}) {
    const id = ++this.id;
    this.ws.send(JSON.stringify({ id, method, params }));
    return new Promise((resolve, reject) => {
      this.waiting.set(id, { resolve, reject });
      setTimeout(() => { if (this.waiting.delete(id)) reject(new Error(method + ' timed out')); }, 30000);
    });
  }
  // Evaluate in the page and return the value (awaits promises).
  async eval(expr) {
    const r = await this.send('Runtime.evaluate', { expression: expr, returnByValue: true, awaitPromise: true, userGesture: true });
    if (r.exceptionDetails) throw new Error('page: ' + (r.exceptionDetails.exception?.description || r.exceptionDetails.text));
    return r.result?.value;
  }
}

// In-page helper: one selector language for every verb. `text=…` picks the
// SMALLEST element whose visible text contains the words, which is what a
// person means by "click Sync now" when the words also sit inside a card.
const FIND = `
window.__find = function(sel) {
  if (!sel.startsWith('text=')) return document.querySelector(sel);
  const want = sel.slice(5).trim().toLowerCase();
  let best = null;
  for (const el of document.querySelectorAll('body *')) {
    const t = (el.innerText || el.value || '').trim().toLowerCase();
    if (!t || !t.includes(want)) continue;
    const r = el.getBoundingClientRect();
    if (!r.width || !r.height) continue;
    // Same words, one inside the other (a full-width <div class="small"> and
    // the <a>‹ Money</a> that is all it holds): take the INNER one. Their text
    // is equal, so "smallest text" cannot separate them, and the wrapper's
    // centre — where a click lands — is empty space beside the link.
    if (!best || t.length < best.len || (t.length === best.len && best.el.contains(el))) best = { el, len: t.length };
  }
  return best && best.el;
};
// The console does not scroll the window: its panes scroll inside a fixed
// viewport, so window.scrollTo is a no-op and a "full page" capture stops at
// the fold. Whatever element actually has overflow is the one to drive.
window.__scroller = function() {
  let best = document.scrollingElement, gap = best ? best.scrollHeight - best.clientHeight : 0;
  for (const el of document.querySelectorAll('body *')) {
    const g = el.scrollHeight - el.clientHeight;
    if (g <= gap) continue;
    const o = getComputedStyle(el).overflowY;
    if (o !== 'auto' && o !== 'scroll') continue;
    best = el; gap = g;
  }
  return best;
};
// How tall the window must be for everything to be on screen at once.
window.__fullHeight = function() {
  let h = Math.max(document.body.scrollHeight, window.innerHeight);
  for (const el of document.querySelectorAll('body *')) {
    const o = getComputedStyle(el).overflowY;
    if (o !== 'auto' && o !== 'scroll') continue;
    h = Math.max(h, el.scrollHeight + (window.innerHeight - el.clientHeight));
  }
  return h;
};
// Where to aim the mouse. An element already on screen is clicked WHERE IT IS:
// scrolling it to centre first was the harness moving the page itself, which
// made "does clicking this move the scroll position?" impossible to ask (the
// calendar's scroll-jump bug, 2026-09-01 — every click read as a 22px jump the
// page had not made). Only something out of view is scrolled to.
window.__box = function(sel) {
  const el = window.__find(sel);
  if (!el) return null;
  const v = el.getBoundingClientRect();
  const onScreen = v.top >= 0 && v.left >= 0 && v.bottom <= window.innerHeight && v.right <= window.innerWidth;
  if (!onScreen) el.scrollIntoView({block: 'center', inline: 'center'});
  const r = el.getBoundingClientRect();
  return {x: r.left + r.width / 2, y: r.top + r.height / 2, w: r.width, h: r.height,
          tag: el.tagName.toLowerCase(), text: (el.innerText || '').trim().slice(0, 60)};
};`;

// -------------------------------------------------------------------- main
(async () => {
  fs.mkdirSync(out, { recursive: true });
  const profile = keep
    ? path.join(ROOT, 'ops/logs/web/browse-profile')
    : fs.mkdtempSync(path.join(os.tmpdir(), 'life-browse-'));
  const port = 9333 + Math.floor(Math.random() * 500);
  const chrome = spawn(CHROME, [
    '--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check',
    '--ignore-certificate-errors', // the hub serves its own tailnet cert
    `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
    `--window-size=${winW},${winH}`, 'about:blank',
  ], { stdio: 'ignore' });

  let ws, cdp, failed = false;
  const problems = [];
  // Chrome is still flushing its profile when we ask for it back, so the
  // delete is best-effort and happens once (the exit handler runs it again).
  let stopped = false;
  const stop = () => {
    if (stopped) return;
    stopped = true;
    try { ws && ws.close(); } catch {}
    try { chrome.kill(); } catch {}
    if (!keep) try { fs.rmSync(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 }); } catch {}
  };
  process.on('exit', stop);

  try {
    const list = await getJSON(`http://127.0.0.1:${port}/json/list`);
    const page = list.find((t) => t.type === 'page');
    if (!page) throw new Error('chrome opened no page target');
    await new Promise((resolve) => {
      ws = new WS(page.webSocketDebuggerUrl, (raw) => cdp.message(raw));
      cdp = new CDP(ws);
      const t = setInterval(() => { if (ws.open) { clearInterval(t); resolve(); } }, 20);
    });

    // Everything the page complains about, kept for the summary.
    cdp.on((m) => {
      if (m.method === 'Runtime.exceptionThrown') {
        const d = m.params.exceptionDetails;
        problems.push('exception: ' + (d.exception?.description || d.text));
      } else if (m.method === 'Runtime.consoleAPICalled' && ['error', 'warning', 'assert'].includes(m.params.type)) {
        problems.push(m.params.type + ': ' + m.params.args.map((a) => a.value ?? a.description ?? a.type).join(' '));
      } else if (m.method === 'Log.entryAdded' && m.params.entry.level === 'error') {
        problems.push('log: ' + m.params.entry.text + ' ' + (m.params.entry.url || ''));
      }
    });
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');
    await cdp.send('Log.enable');
    await cdp.send('Emulation.setDeviceMetricsOverride', { width: winW, height: winH, deviceScaleFactor: scale, mobile: false });
    await cdp.send('Page.addScriptToEvaluateOnNewDocument', { source: FIND });
    if (dark) await cdp.send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: 'dark' }] });
    if (web) {
      const TYPES = { '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html; charset=utf-8' };
      cdp.on((m) => {
        if (m.method !== 'Fetch.requestPaused') return;
        const { requestId, request } = m.params;
        const u = new URL(request.url);
        // The ?token= visit still goes to the hub: it sets the cookie.
        let f = '';
        if (u.pathname.startsWith('/static/')) f = path.join(web, u.pathname.slice('/static/'.length));
        else if (u.pathname === '/' && !u.searchParams.has('token')) f = path.join(web, 'index.html');
        if (!f || !f.startsWith(web) || !fs.existsSync(f)) return cdp.send('Fetch.continueRequest', { requestId }).catch(() => {});
        cdp.send('Fetch.fulfillRequest', {
          requestId, responseCode: 200,
          responseHeaders: [{ name: 'Content-Type', value: TYPES[path.extname(f)] || 'application/octet-stream' }, { name: 'Cache-Control', value: 'no-store' }],
          body: fs.readFileSync(f).toString('base64'),
        }).catch(() => {});
      });
      const origin = new URL(hubBase).origin;
      await cdp.send('Fetch.enable', { patterns: [{ urlPattern: origin + '/static/*' }, { urlPattern: origin + '/*', resourceType: 'Document' }] });
    }

    let n = 0;
    for (const step of steps) {
      const sp = step.indexOf(' ');
      const verb = (sp < 0 ? step : step.slice(0, sp)).toLowerCase();
      const arg = sp < 0 ? '' : step.slice(sp + 1).trim();
      n++;
      const tag = String(n).padStart(2, '0');
      const line = await run(cdp, verb, arg, out, tag, hubBase, winW, winH);
      console.log(`${tag} ${verb}${arg ? ' ' + arg : ''}${line ? ' → ' + line : ''}`);
    }
  } catch (e) {
    failed = true;
    console.error('browse: FAILED — ' + e.message);
  }

  if (problems.length) {
    console.log('\npage problems (' + problems.length + '):');
    // The same error repeats every poll; say it once with a count.
    const seen = new Map();
    for (const p of problems) seen.set(p, (seen.get(p) || 0) + 1);
    for (const [p, c] of seen) console.log('  ' + (c > 1 ? `(x${c}) ` : '') + p.slice(0, 400));
  } else {
    console.log('\npage problems: none');
  }
  stop();
  process.exit(failed || (strict && problems.length) ? 1 : 0);
})();

async function run(cdp, verb, arg, out, tag, hubBase, winW, winH) {
  switch (verb) {
    case 'goto': {
      const url = arg.startsWith('#') ? hubBase + arg : arg;
      await cdp.send('Page.navigate', { url });
      await settle(cdp);
      return await cdp.eval('document.title');
    }
    case 'wait': {
      if (/^\d+$/.test(arg)) { await sleep(Number(arg)); return arg + 'ms'; }
      for (let i = 0; i < 80; i++) {
        if (await cdp.eval(`!!window.__find(${JSON.stringify(arg)})`)) return 'appeared';
        await sleep(250);
      }
      throw new Error('waited 20s, never appeared: ' + arg);
    }
    case 'click':
    case 'hover': {
      const box = await cdp.eval(`JSON.stringify(window.__box(${JSON.stringify(arg)}))`);
      if (!box || box === 'null') throw new Error('no element: ' + arg);
      const b = JSON.parse(box);
      await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: b.x, y: b.y });
      if (verb === 'hover') return `${b.tag} "${b.text}"`;
      for (const type of ['mousePressed', 'mouseReleased']) {
        await cdp.send('Input.dispatchMouseEvent', { type, x: b.x, y: b.y, button: 'left', clickCount: 1 });
      }
      await settle(cdp);
      return `${b.tag} "${b.text}"`;
    }
    case 'fill': {
      const sp = arg.indexOf(' ');
      const sel = sp < 0 ? arg : arg.slice(0, sp);
      const text = sp < 0 ? '' : arg.slice(sp + 1);
      const box = await cdp.eval(`JSON.stringify(window.__box(${JSON.stringify(sel)}))`);
      if (!box || box === 'null') throw new Error('no element: ' + sel);
      const b = JSON.parse(box);
      for (const type of ['mousePressed', 'mouseReleased']) {
        await cdp.send('Input.dispatchMouseEvent', { type, x: b.x, y: b.y, button: 'left', clickCount: 3 });
      }
      await cdp.send('Input.insertText', { text });
      // A framework that listens for `input` needs the event even when the
      // text arrived through the protocol rather than a keystroke.
      await cdp.eval(`(function(){const e=window.__find(${JSON.stringify(sel)});
        e.dispatchEvent(new Event('input',{bubbles:true}));
        e.dispatchEvent(new Event('change',{bubbles:true}));})()`);
      return JSON.stringify(text.slice(0, 40));
    }
    case 'press': {
      const keys = {
        Enter: [13, 'Enter'], Escape: [27, 'Escape'], Tab: [9, 'Tab'], Space: [32, ' '],
        ArrowDown: [40, 'ArrowDown'], ArrowUp: [38, 'ArrowUp'], ArrowLeft: [37, 'ArrowLeft'], ArrowRight: [39, 'ArrowRight'],
        Backspace: [8, 'Backspace'], Delete: [46, 'Delete'],
      };
      // A single letter is a plain keystroke (crossword grids listen on keydown)
      let k = keys[arg] || keys[arg[0].toUpperCase() + arg.slice(1)];
      let text = arg === 'Enter' ? '\r' : arg === 'Space' ? ' ' : undefined;
      if (!k && /^[a-zA-Z]$/.test(arg)) { k = [arg.toUpperCase().charCodeAt(0), arg]; text = arg; }
      if (!k) throw new Error('unknown key: ' + arg);
      const code = k[1].length === 1 ? (k[1] === ' ' ? 'Space' : 'Key' + k[1].toUpperCase()) : k[1];
      for (const type of ['keyDown', 'keyUp']) {
        await cdp.send('Input.dispatchKeyEvent', { type, windowsVirtualKeyCode: k[0], key: k[1], code, text });
      }
      await settle(cdp);
      return 'sent';
    }
    case 'scroll': {
      if (arg === 'bottom') await cdp.eval('(function(){const s=window.__scroller(); s.scrollTop = s.scrollHeight;})()');
      else if (arg === 'top') await cdp.eval('window.__scroller().scrollTop = 0');
      else if (/^-?\d+$/.test(arg)) await cdp.eval(`window.__scroller().scrollTop += ${arg}`);
      else await cdp.eval(`window.__find(${JSON.stringify(arg)}).scrollIntoView({block:'center'})`);
      await sleep(300);
      return String(await cdp.eval('Math.round(window.__scroller().scrollTop)'));
    }
    // `scroll` assigns scrollTop; this one turns a real wheel over an element,
    // which is not the same input. A user's wheel arms Chrome's scroll
    // anchoring and leaves a user-scroll flag on the box, and code that puts
    // the position back after a repaint can behave differently under it — so a
    // "does the page jump when I click?" bug has to be driven this way.
    //   wheel <±px> [sel]     default sel: the viewport centre
    case 'wheel': {
      const sp = arg.indexOf(' ');
      const dy = parseInt(sp < 0 ? arg : arg.slice(0, sp), 10);
      const sel = sp < 0 ? '' : arg.slice(sp + 1).trim();
      let x = winW / 2, y = winH / 2;
      if (sel) {
        const box = await cdp.eval(`JSON.stringify(window.__box(${JSON.stringify(sel)}))`);
        if (!box || box === 'null') throw new Error('no element: ' + sel);
        const b = JSON.parse(box); x = b.x; y = b.y;
      }
      await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x, y });
      await cdp.send('Input.dispatchMouseEvent', { type: 'mouseWheel', x, y, deltaX: 0, deltaY: dy });
      await sleep(300);
      return `${dy > 0 ? '+' : ''}${dy} at ${Math.round(x)},${Math.round(y)}`;
    }
    case 'shot': {
      const name = arg || 'shot';
      // captureBeyondViewport: the console's panes are long, and a picture
      // that stops at 1000px is how a broken footer stays invisible.
      const h = Math.min(await cdp.eval('window.__fullHeight()'), 8000);
      await cdp.send('Emulation.setDeviceMetricsOverride', { width: winW, height: h, deviceScaleFactor: scale, mobile: false });
      await sleep(200);
      const r = await cdp.send('Page.captureScreenshot', { format: 'png' });
      await cdp.send('Emulation.setDeviceMetricsOverride', { width: winW, height: winH, deviceScaleFactor: scale, mobile: false });
      const f = path.join(out, `${tag}-${name}.png`);
      fs.writeFileSync(f, Buffer.from(r.data, 'base64'));
      return path.relative(process.cwd(), f) + ` (${winW}x${h})`;
    }
    // The viewport as it stands. `shot` grows the window to the page's full
    // height first, and that relayout re-runs hit-testing — which drops
    // whatever the pointer was on: a hover, an open menu, a tooltip. So a
    // picture of a HOVER STATE has to be taken at the window's own size
    // (2026-08-31, photographing the plot tooltips).
    case 'shotv': {
      const r = await cdp.send('Page.captureScreenshot', { format: 'png' });
      const f = path.join(out, `${tag}-${arg || 'shot'}.png`);
      fs.writeFileSync(f, Buffer.from(r.data, 'base64'));
      return path.relative(process.cwd(), f) + ` (${winW}x${winH})`;
    }
    case 'dom': {
      const html = await cdp.eval('document.documentElement.outerHTML');
      const f = path.join(out, `${tag}-${arg || 'dom'}.html`);
      fs.writeFileSync(f, html);
      return path.relative(process.cwd(), f) + ` (${html.length} bytes)`;
    }
    case 'text': {
      const t = await cdp.eval(`(function(){const e=window.__find(${JSON.stringify(arg)}); return e ? (e.innerText||e.value||'') : null;})()`);
      if (t === null) throw new Error('no element: ' + arg);
      return JSON.stringify(t.slice(0, 2000));
    }
    case 'eval':
      return JSON.stringify(await cdp.eval(arg));
    default:
      throw new Error('unknown step: ' + verb);
  }
}

// The console redraws from a poll and every page fills itself from the API
// after it draws, so "the click landed" is not "the screen is what it will
// be": a shot taken on the old rule photographed an empty <main> and a click
// missed a row that had not arrived yet. Wait until the visible text stops
// changing (two equal readings), then one frame.
async function settle(cdp, max = 40) {
  // Watch the ROUTED REGION, not the body: the nav renders instantly and its
  // text never changes, so a body-length check reads "stable" while <main> is
  // still empty — which is how the first version photographed a blank Money
  // page and then failed to click a row that had not arrived.
  const probe = '(function(){const v=document.querySelector("#view")||document.body; return (v.innerText||"").length;})()';
  const seen = [];
  for (let i = 0; i < max; i++) {
    await sleep(250);
    let n;
    try { n = await cdp.eval(probe); } catch { continue; }
    seen.push(n);
    if (seen.length > 3) seen.shift();
    if (seen.length === 3 && n > 0 && seen.every((x) => x === n) && i >= 4) break;
  }
  try { await cdp.eval('new Promise(r => requestAnimationFrame(() => r(1)))'); } catch {}
}

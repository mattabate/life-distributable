// The decider code rides only on a request that decides: app.js api() used
// to put X-Life-Decider on every call the console made. app.js boots the page
// at load, so only its api section and the request helpers are run here, with
// fetch and localStorage stubbed; views/asks.js (withDecider) loads whole.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const store = { 'life.decider': 'TESTCODE' };
globalThis.localStorage = {
  getItem: k => store[k] ?? null,
  setItem: (k, v) => { store[k] = v; },
  removeItem: k => { delete store[k]; },
};
globalThis.conn = () => {};
globalThis.seenBuild = () => {};
globalThis.toast = () => {};
const sent = [];
let refuseOnce = false;
let dropOnce = false;
globalThis.fetch = async (url, init) => {
  sent.push({ url, headers: init.headers || {} });
  if (dropOnce) { dropOnce = false; throw new TypeError('Failed to fetch'); }
  const deny = refuseOnce; refuseOnce = false;
  return {
    ok: !deny, status: deny ? 403 : 200,
    headers: { get: () => null },
    text: async () => (deny ? '{"error":"needs the code"}' : '{"ok":true}'),
  };
};

const app = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');
const apiSection = app.slice(app.indexOf('const decider = {'), app.indexOf('// ---------- the console notices'));
const helpers = app.split('\n').filter(l => /^const (get|post) = /.test(l)).join('\n');
assert.ok(apiSection.includes('async function api(') && helpers.includes('const post'), 'app.js api section moved');
vm.runInThisContext(apiSection + '\n' + helpers + '\nglobalThis.api = api; globalThis.get = get; globalThis.post = post; globalThis.decider = decider;', { filename: 'app.js' });
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'views', 'asks.js'), 'utf8'), { filename: 'asks.js' });

const last = () => sent[sent.length - 1].headers['X-Life-Decider'];

test('an ordinary read or write carries no decider code', async () => {
  await get('/board?surface=web');
  assert.equal(last(), undefined);
  await post('/prompts?via=web', { text: 'hi' });
  assert.equal(last(), undefined);
  await api('/settings', { method: 'PATCH', body: '{}' });
  assert.equal(last(), undefined);
});

test('a send through withDecider carries the stored code', async () => {
  await withDecider(d => post('/actions/x/approve?via=web', {}, d));
  assert.equal(last(), 'TESTCODE');
  // and the option does not leak into the next ordinary call
  await get('/board');
  assert.equal(last(), undefined);
});

// A retried write could land twice. Every try of one write carries the same
// X-Life-Request, so the hub runs it once (server/replay.go); a read carries
// none, and the next write a new one.
test('a write retried after a dropped link keeps its one request name', async () => {
  sent.length = 0;
  dropOnce = true;
  await post('/threads/t1/messages?via=web', { text: 'hi' });
  assert.equal(sent.length, 2);
  const id = sent[0].headers['X-Life-Request'];
  assert.match(id, /^[0-9a-f-]{36}$/);
  assert.equal(sent[1].headers['X-Life-Request'], id);
  await get('/board');
  assert.equal(sent[2].headers['X-Life-Request'], undefined);
  await post('/threads/t1/messages?via=web', { text: 'again' });
  assert.notEqual(sent[3].headers['X-Life-Request'], id);
});

test('after a refusal and a checked new code, the retry carries the new code', async () => {
  globalThis.prompt = () => 'newcode';
  refuseOnce = true;
  sent.length = 0;
  await withDecider(d => post('/prompts?via=web', { outcome: 'approved' }, d));
  // refused send, the /decider check with the candidate, the retry
  assert.equal(sent.length, 3);
  assert.equal(sent[0].headers['X-Life-Decider'], 'TESTCODE');
  assert.equal(sent[1].headers['X-Life-Decider'], 'NEWCODE');
  assert.equal(sent[2].headers['X-Life-Decider'], 'NEWCODE');
});

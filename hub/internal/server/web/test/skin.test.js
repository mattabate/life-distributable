// skin.js strips the UI's own emoji marks from what the page draws, but never
// from code: a code block or an approval's What it runs is what the owner
// copies and what runs. A tree of text nodes is stubbed, just enough for its walker. The fixtures hold no personal data.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

// An element: a tag, its ancestors' tags, its text nodes.
const elem = (tags, texts) => {
  const e = {
    nodeType: 1,
    closest: sel => sel.split(',').some(s => tags.includes(s.trim())) ? e : null,
  };
  e.texts = texts.map(v => ({ nodeType: 3, nodeValue: v, parentElement: e }));
  return e;
};
const p = elem(['p'], ['🔴 Approve this']);
const pre = elem(['pre', 'details'], ['echo "⚠ keep" ']);
const code = elem(['code', 'p'], ['📎 x']);
const body = { nodeType: 1, closest: () => null };
const all = [p, pre, code].flatMap(e => e.texts);
globalThis.NodeFilter = { SHOW_TEXT: 4 };
globalThis.MutationObserver = class { observe() {} };
globalThis.document = {
  body,
  createTreeWalker: root => {
    const nodes = root === body ? all : root.texts;
    let i = 0;
    return { nextNode: () => nodes[i++] || null };
  },
  addEventListener() {},
};
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'skin.js'), 'utf8'), { filename: 'skin.js' });

test('the UI marks go, code stays byte for byte', () => {
  assert.equal(p.texts[0].nodeValue, 'Approve this');
  assert.equal(pre.texts[0].nodeValue, 'echo "⚠ keep" ');
  assert.equal(code.texts[0].nodeValue, '📎 x');
});

// Math in replies and cards (ui.js mdMath): $...$ and $$...$$ typeset by the
// vendored KaTeX, money and code left as written. Its own file because it
// loads katex into the global, which ui.test.js runs without.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'vendor', 'katex', 'katex.min.js'), 'utf8'), { filename: 'katex.min.js' });
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'ui.js'), 'utf8'), { filename: 'ui.js' });

const isMath = h => /class="katex"/.test(h);

test('inline and display math are typeset', () => {
  assert.ok(isMath(mdInline('Euler: $e^{i\\pi} + 1 = 0$.')));
  assert.ok(/katex-display/.test(mdInline('So $$\\sum_{n=1}^\\infty \\frac{1}{n^2} = \\frac{\\pi^2}{6}$$ holds.')));
  assert.ok(isMath(mdInline('$x$')));
  // TeX's < and & are TeX, not HTML.
  assert.ok(isMath(mdInline('$a < b$')) && !/&amp;lt;/.test(mdInline('$a < b$')));
});

test('dollar amounts stay money', () => {
  for (const s of ['$12 and $15', 'it is $5-$10 a month', '$12+$15=$27', 'costs $12.', 'between $1,200 and $3,400 (est.)', '$PATH:$HOME']) {
    const h = mdInline(s);
    assert.ok(!isMath(h), s);
    assert.equal((h.match(/\$/g) || []).length, (s.match(/\$/g) || []).length, s);
  }
});

test('code is never math', () => {
  assert.ok(!isMath(mdInline('run `echo $x$` now')));
  assert.ok(isMath(mdInline('`echo $HOME` and $y$')));
  assert.ok(!isMath(md('```\n$$x$$\n$a$\n```')));
});

test('a display block over several lines; unclosed stays text', () => {
  const h = md('Sum:\n$$\n\\int_0^1 x\\,dx\n= \\tfrac12\n$$\nDone.');
  assert.ok(/<div class="md-math">.*katex-display/.test(h));
  assert.ok(/<p>Done\.<\/p>/.test(h));
  assert.ok(!isMath(md('$$\n\\int_0^1 x')));
});

// The scripts share one global scope, so a later file's function of the same
// name silently replaces an earlier one (a page script's own mathHTML once
// replaced the card renderer's).
test('no two console scripts declare the same top-level function', () => {
  const web = path.join(__dirname, '..');
  const files = [...fs.readdirSync(web).filter(f => f.endsWith('.js')),
    ...fs.readdirSync(path.join(web, 'views')).filter(f => f.endsWith('.js')).map(f => 'views/' + f)];
  const seen = new Map(), dup = [];
  for (const f of files) {
    for (const m of fs.readFileSync(path.join(web, f), 'utf8').matchAll(/^(?:async\s+)?function\s+(\w+)/gm)) {
      if (seen.has(m[1])) dup.push(`${m[1]}: ${seen.get(m[1])} and ${f}`); else seen.set(m[1], f);
    }
  }
  assert.deepEqual(dup, []);
});

test('TeX KaTeX refuses shows as written', () => {
  assert.equal(mdInline('$\\notacommand{x}$'), '$\\notacommand{x}$');
});

// Tests for views/config.js's Team card, the org chart: the owner on top, a
// card per role that reports to the owner, its own reports listed inside a
// step in per level, the emblems of the goals it owns on its top line.
// `ops/webcheck.sh` runs these (node --test). The roles and goals below are
// invented and hold no personal data.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

globalThis.document = { addEventListener() {}, querySelector: () => null, querySelectorAll: () => [], getElementById: () => null };
const load = f => vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', f), 'utf8'), { filename: f });
load('ui.js');
load('views/goals.js');
load('views/config.js');

const st = { team_mode: true, plain_sessions: false };
const goals = [
  { id: 'get-fit', title: 'Get fit', status: 'active', emblem: { symbol: 'goal', hue: 20 } },
  { id: 'eat-well', title: 'Eat well', status: 'active', emblem: { symbol: 'goal', hue: 140 } },
];
const personas = [
  { key: 'staff', role: 'Chief of staff', reports_to: '', goals: [], session: 'calendar' },
  { key: 'coach', role: 'Fitness coach', reports_to: 'staff', goals: ['get-fit', 'eat-well'], session: 'sam-1' },
  { key: 'chef', role: 'Chef', reports_to: 'coach', goals: ['eat-well', 'gone'], session: '' },
  { key: 'scout', role: 'Researcher', reports_to: 'ghost', goals: [], session: 'kim-2' },
];
const sessions = [
  { id: 'calendar', persona: 'staff', title: 'Alex' },
  { id: 'sam-1', persona: 'coach', title: 'Sam' },
  { id: 'kim-2', persona: 'scout', title: 'Kim' },
];
const cards = html => html.split('<div class="cfg-ocard">').slice(1);

test('team: the owner on top, a card per role under the owner, reports inside', () => {
  const html = cfgTeamHTML(st, personas, sessions, goals);
  assert.match(html, /<div class="cfg-me">You<\/div>/);
  assert.match(html, /setSetting\('team_mode'/, 'the switches stay');
  const cs = cards(html);
  assert.equal(cs.length, 2, 'Alex, and Kim whose boss is no role');
  assert.match(cs[0], /class="nm" href="#\/sessions\/calendar">Alex</);
  assert.match(cs[0], /class="role muted"[^>]*>Chief of staff</);
  assert.match(cs[1], />Kim</);
  // Sam reports to Alex (a row in Alex's card), the chef to Sam (a step in).
  const reps = cs[0].split('<div class="cfg-orep"').slice(1);
  assert.equal(reps.length, 2);
  assert.match(reps[0], /^>/, 'Sam is the first level: no indent');
  assert.match(reps[0], /<strong>Sam<\/strong><span class="muted">Fitness coach<\/span>/);
  assert.match(reps[1], /^ style="padding-left:10px"/, 'the chef a step in');
  assert.match(reps[1], /<span class="cfg-who"><strong>Chef<\/strong>/, 'no session: a name, not a link');
});

test('team: a role carries the emblems of the goals it owns, unknown ids dropped', () => {
  const html = cfgTeamHTML(st, personas, sessions, goals);
  const sam = html.split('<strong>Sam</strong>')[1].split('<div class="cfg-orep"')[0];
  assert.equal((sam.match(/href="#\/goals\//g) || []).length, 2);
  assert.match(sam, /title="Get fit"/);
  const chef = html.split('<strong>Chef</strong>')[1];
  assert.equal((chef.split('</div>')[0].match(/href="#\/goals\//g) || []).length, 1, 'eat-well only');
});

test('team: off, only the switch; no roles, the empty line', () => {
  const off = cfgTeamHTML({ team_mode: false }, null, null, goals);
  assert.ok(!off.includes('cfg-org') && off.includes("setSetting('team_mode'"));
  assert.match(cfgTeamHTML(st, [], [], goals), /No roles yet/);
  assert.match(cfgTeamHTML(st, null, [], goals), /the team/);
});

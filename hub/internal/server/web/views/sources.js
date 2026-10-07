// life hub — laptop console: one source's page, `#/sources/<group>/<id>`.
// views.sources. The sources themselves are cards on Configuration
// (views/config.js), and a bare #/sources lands there (app.js render). A
// source is a SOURCE, not a subject, and its page carries the catalog prose —
// how it gets in, where it lands, rows by kind — the same sections the
// phone's SourceDetail draws.
'use strict';
// ================= sources =================

// One account = one line: what it is, then what it gives, then how fresh.
// Not a pill — a wall of grey pills looks bad; on a white card the name alone
// carries it.
function sourceChip(a) {
  const label = a.url ? `<a href="${esc(a.url)}" target="_blank" rel="noopener">${esc(a.label)}</a>` : esc(a.label);
  const bits = [];
  if (a.detail) bits.push(esc(a.detail));
  if (a.last) bits.push(esc(ago(a.last)));
  return `<div class="src-acct"><span class="src-acct-name">${label}</span>${bits.length ? ` <span class="muted">${bits.join(' · ')}</span>` : ''}</div>`;
}

function sourceStatus(s) {
  const cls = s === 'failing' ? 'bad' : s === 'connected' || s === 'live' ? 'ok' : 'manual';
  return `<span class="dot src-dot ${cls}" title="${esc(s)}"></span>`;
}

// What a failing connector says. "Connected" used to mean only that a
// credential existed, and the newest row is the newest of ANY kind —
// including rows the hub writes without asking the upstream anything — so a
// connector can read "connected · 2m ago" while every call comes back
// refused. The point of the line is not that there is an error: it is HOW
// OLD the numbers are.
function sourceFailBits(s) {
  const n = s.fails || 1; // absent means one, and `fails` is omitted at zero
  const bits = [`${n} ${n === 1 ? 'try' : 'tries'} failed`];
  if (s.failing_since) bits.push(`since ${ago(s.failing_since)}`);
  bits.push(s.last_ok ? `last worked ${ago(s.last_ok)}` : 'never worked');
  return bits.join(' · ');
}

function sourceFail(s) {
  if (s.status !== 'failing') return '';
  return `<span class="src-fail">Not syncing — ${esc(sourceFailBits(s))}</span>`;
}

function sourceHash(g, s) {
  return `#/sources/${encodeURIComponent(g.id)}/${encodeURIComponent(s.id)}`;
}

// One source's page: status (with what the service said, when it is refusing
// us), what it is connected to, where it comes from, where it lands, rows by
// kind.
function sourcePageHTML(g, s) {
  const as = s.accounts || [];
  const kinds = (s.kinds || []).map(k => `<tr><td class="mono">${esc(k.kind)}</td><td class="small muted">${esc(k.note || '')}</td>
      <td class="small muted src-last">${k.first ? esc(k.first.slice(0, 10)) + ' → ' + esc((k.last || '').slice(0, 10)) : ''}</td><td class="n">${num(k.n)}</td></tr>`).join('');
  const status = [
    `<div>${sourceStatus(s.status)}${esc(s.status)}${sourceFail(s)}</div>`,
    s.error ? `<div class="src-fail" style="margin:6px 0 0">What the service said: <code>${esc(s.error)}</code></div>` : '',
    `<div class="small muted mt6">${s.total != null ? num(s.total) + ' rows' : ''}${s.last ? ` · newest row ${esc(ago(s.last))}` : ''}${s.last_ok ? ` · last worked ${esc(ago(s.last_ok))}` : ''}</div>`,
  ].join('');
  return `<div class="pane wide">
    <div class="spread"><h2><a href="#/config">Configuration</a> / ${esc(s.title || s.id)}</h2><span class="small muted">${esc(g.tag || g.title)}</span></div>
    <section class="src-card"><div class="src-page">
      ${status}
      ${as.length ? `<h4>Connected to</h4>${as.map(sourceChip).join('')}` : ''}
      <h4>Where it comes from</h4><div class="small">${esc(s.from || '')}</div>
      <h4>Where it is stored</h4><div class="small">${esc(s.storage || '')}</div>
      ${kinds ? `<h4>What is kept (rows by kind)</h4><table class="src-kinds">${kinds}</table>` : ''}
    </div></section>
  </div>`;
}

async function drawSources(view, rest) {
  const d = await get('/sources');
  const gid = decodeURIComponent(rest?.[0] || ''), sid = decodeURIComponent(rest?.[1] || '');
  const g = (d.groups || []).find(x => x.id === gid);
  const s = g && (g.sources || []).find(x => x.id === sid);
  // Gone from the payload (a connector the owner disconnected): back to the cards.
  if (!s) { location.replace('#/config'); return; }
  view.innerHTML = sourcePageHTML(g, s);
}

views.sources = { draw: drawSources, redraw: () => render() };

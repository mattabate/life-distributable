// life hub — laptop console: Sources ("what am I connected to right now?").
// views.sources. One compact table per section, one row per source, one line
// per thing that source is pointed at (every connected account is listed).
// A row is a SOURCE, not a subject — so finance is SimpleFin, the Drive
// folder with its folders listed under it, and the tables that came from
// neither. Unconnected sources are not in the payload at all. The catalog
// prose (how it gets in, where it lands, rows by kind) is a PAGE behind each
// row, `#/sources/<group>/<id>`, not an in-place fold — the same as the
// phone's SourceDetail.
'use strict';
// ================= sources =================

// One account = one line: what it is, then what it gives, then how fresh.
// Not a pill — a wall of grey pills looks bad; on a white card the name alone
// carries it.
function sourceChip(a) {
  // The link is its own click (a repo name opens GitHub, not the source's
  // page); everywhere else on the row is the row's.
  const label = a.url ? `<a href="${esc(a.url)}" target="_blank" rel="noopener" onclick="event.stopPropagation()">${esc(a.label)}</a>` : esc(a.label);
  const bits = [];
  if (a.detail) bits.push(esc(a.detail));
  if (a.last) bits.push(esc(ago(a.last)));
  return `<div class="src-acct"><span class="src-acct-name">${label}</span>${bits.length ? ` <span class="muted">${bits.join(' · ')}</span>` : ''}</div>`;
}

function sourceStatus(s) {
  const cls = s === 'failing' ? 'bad' : s === 'connected' || s === 'live' ? 'ok' : 'manual';
  return `<span class="dot src-dot ${cls}" title="${esc(s)}"></span>`;
}

// What a failing connector says on its row (2026-09-01). "Connected" used to
// mean only that a credential existed, and the Newest column is the newest row
// of ANY kind — including rows the hub writes without asking the upstream
// anything — so X read "connected · 2m ago" for two days while every X call
// came back 402 and the follower curve sat frozen at Aug 30. The point of the
// line is not that there is an error: it is HOW OLD the numbers below it are.
function sourceFail(s) {
  if (s.status !== 'failing') return '';
  const n = s.fails || 1; // absent means one, and `fails` is omitted at zero
  const bits = [`${n} ${n === 1 ? 'try' : 'tries'} failed`];
  if (s.failing_since) bits.push(`since ${ago(s.failing_since)}`);
  bits.push(s.last_ok ? `last worked ${ago(s.last_ok)}` : 'never worked');
  return `<span class="src-fail">Not syncing — ${esc(bits.join(' · '))}</span>`;
}

function sourceHash(g, s) {
  return `#/sources/${encodeURIComponent(g.id)}/${encodeURIComponent(s.id)}`;
}

// A row is the door to the source's page; only the account links inside it
// are their own click (sourceChip), so a repo name still opens GitHub.
function sourceRow(g, s) {
  const as = s.accounts || [];
  const cells = `<td>${as.length ? as.map(sourceChip).join('') : '<span class="muted">—</span>'}</td>`;
  return `<tr class="src-row click" onclick="location.hash='${sourceHash(g, s)}'">
      <td class="src-name">${sourceStatus(s.status)}<strong>${esc(s.title || s.id)}</strong>${sourceFail(s)}</td>
      ${cells}
      <td class="n small muted">${s.total != null ? num(s.total) : ''}</td>
      <td class="n small muted src-last">${s.last ? esc(ago(s.last)) : ''}</td>
    </tr>`;
}

// Column widths are declared, not measured: the table is `table-layout: fixed`
// so long prose in one cell cannot push the columns around — opening X /
// Twitter used to shift the whole header. 33% is wide enough
// that no source name wraps ("Semantic Scholar (citation context)").
const SRC_COLGROUP = '<colgroup><col style="width:33%"><col><col style="width:76px"><col style="width:92px"></colgroup>';

// A section is one white card: its own header, then the table inside it.
function sourceGroupHTML(g) {
  const rows = (g.sources || []).map(s => sourceRow(g, s)).join('');
  return `<section class="src-card">
    <header class="src-head">
      <h3>${esc(g.title)}</h3>
      ${g.note ? `<span class="small muted">${esc(g.note)}</span>` : ''}
    </header>
    <table class="src-table">${SRC_COLGROUP}<thead><tr><th>Source</th><th>Connected to</th><th class="n">Rows</th><th class="n">Newest</th></tr></thead>
      <tbody>${rows || '<tr><td colspan="4" class="muted small">Nothing connected.</td></tr>'}</tbody></table>
  </section>`;
}

// One source's page: the same sections the phone's SourceDetail draws, in the
// same order — status (with what the service said, when it is refusing us),
// what it is connected to, where it comes from, where it lands, rows by kind.
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
    <div class="spread"><h2><a href="#/sources">Sources</a> / ${esc(s.title || s.id)}</h2><span class="small muted">${esc(g.title)}</span></div>
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
  const groups = (d.groups || []).filter(g => (g.sources || []).length);
  if (rest && rest.length >= 2 && rest[1]) {
    const gid = decodeURIComponent(rest[0]), sid = decodeURIComponent(rest[1]);
    const g = groups.find(x => x.id === gid);
    const s = g && g.sources.find(x => x.id === sid);
    if (s) { view.innerHTML = sourcePageHTML(g, s); return; }
    // Gone from the payload (a connector the owner disconnected): back to the list.
  }
  const all = groups.flatMap(g => g.sources);
  const bad = all.filter(s => s.status === 'failing').length;
  // The count led with "N connected" — which is the claim this page was making
  // wrongly. It counts sources; whether they WORK is the red half of the legend.
  const head = bad ? `${all.length} sources · ${bad} not syncing` : `${all.length} sources`;
  view.innerHTML = `<div class="pane wide">
    <div class="spread"><h2>Sources</h2>
      <span class="small muted src-legend">${head}<span class="dot src-dot ok"></span>live feed${bad ? '<span class="dot src-dot bad"></span>refusing us' : ''}<span class="dot src-dot manual"></span>you maintain it<span class="src-sep">·</span>a row opens the source</span></div>
    ${groups.map(sourceGroupHTML).join('')}
  </div>`;
}

views.sources = { draw: drawSources, redraw: () => render() };

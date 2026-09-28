// life hub — laptop console: one scheduled-job run. views.runs.
'use strict';
// ================= runs =================
// A scheduled job (ops/schedule.json) has no session, so a proposal it made
// had nothing to open on — the phone card had no chevron at all. This is the
// nearest thing to the chat where the approval was suggested: what the job
// said (the prose before its JSON envelope), its digest summary, every
// finding it filed (gated ones became the proposals) and what it said only
// the owner can do.
// Reached from an action card's "Open the run" (ui.js actionHTML) — no nav
// tab, no list: `lifectl runs` is the list.
views.runs = {
  draw: (view, rest) => runDetail(view, rest[0]),
  redraw: () => render(),
};

async function runDetail(view, id) {
  if (!id) { view.innerHTML = `<div class="pane wide">${emptyHTML('Which run? A proposal card links here.')}</div>`; return; }
  const r = await get('/runs/' + encodeURIComponent(id));
  const took = r.finished_at ? Math.max(1, Math.round((new Date(r.finished_at) - new Date(r.started_at)) / 1000)) + 's' : 'still running';
  const state = r.ok === false ? pill('failed', 'needs') : r.ok ? pill('ok', 'done') : pill('running');
  const findings = (r.findings || []).map(f => `<div class="card">
      <div class="spread"><strong>${mdInline(f.title)}</strong>${pill(f.kind, kindPill[f.kind])}</div>
      <div class="small mt6">${md(f.detail || '')}</div></div>`).join('');
  view.innerHTML = `<div class="pane wide">
    <div class="row" style="margin-bottom:10px"><a href="#/sessions" class="btn sm" style="text-decoration:none">←</a>
      <h2 class="grow m0">⚙ ${esc(r.job)} job · run ${esc(r.id)}</h2>${state}</div>
    <div class="card small muted">${when(r.started_at)} · took ${took} · ${usd(r.cost_usd || 0)}${r.session_id ? ' · claude session ' + esc(r.session_id) : ''}</div>
    ${r.error ? card(esc(r.error), 'err') : ''}
    ${r.text ? card(`<div class="small muted" style="margin-bottom:6px">What it said</div>${md(r.text)}`) : ''}
    ${r.summary ? card(`<div class="small muted" style="margin-bottom:6px">Summary</div>${md(r.summary)}`) : ''}
    ${findings ? `<h3>Findings · ${r.findings.length}</h3>${findings}` : ''}
    ${(r.needs_you || []).length ? `<h3>Only you can</h3><div class="card">${md(r.needs_you.map(n => '- ' + n).join('\n'))}</div>` : ''}
    ${r.output ? `<details class="card"><summary class="muted small">Raw output</summary><pre class="io mono mt6">${esc(r.output)}</pre></details>` : ''}
  </div>`;
}

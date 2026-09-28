// life hub — laptop console: the handlers every surface's ask and approval
// button calls. The cards themselves are drawn by ui.js (askHTML, actionHTML)
// wherever they appear — today that is inside a session's chat.
//
// NOT A PAGE any more. Until 2026-09-02 this file also registered `views.asks`
// — the Your turn page, one stack of every approval and ask bundled by
// session. It went because Your turn is already shown at the top of
// Sessions. Everything it did is now done by two things it was always
// duplicating — the Sessions list's own "Your turn" group (views/threads.js
// `drawList`), which names each session, counts its cards, prints the card's
// title AND its description, and links straight to it; and the cards
// themselves inside the chat, where Approve / Deny / Done / Respond have
// always lived. The nav's red oval moved to Sessions with the page.
'use strict';

// The hub token cannot decide a proposal on its own: a send that carries an
// approve/deny — a card's pick in the composer (views/threads.js chatSend),
// or a job proposal's direct Approve below — is refused (403) until the
// decider code rides with it. This asks for the code once per browser, then
// this browser remembers it (app.js `decider`, sent on every request). The
// code is NOT cleared before asking (2026-08-30): it used to be, so
// dismissing the box threw away a code that worked and asked again on the
// next approval. Only a code the owner actually retypes replaces the stored
// one.
async function withDecider(send) {
  try {
    return await send();
  } catch (e) {
    if (e.status !== 403) throw e;
    const code = prompt(decider.get()
      ? 'That decider code is WRONG — the hub refused it. Fix it here (ops/decider-set.sh printed it):'
      : 'Decider code (ops/decider-set.sh printed it; the phone has it in Settings):', decider.get());
    if (!code) throw e;
    decider.set(code);
    return await send();
  }
}

// A proposal with no session behind it (a scheduled job's) has no composer to
// arm, so its card and its calendar row decide it here, directly. A session's
// own proposal never comes this way: its buttons arm the composer
// (armActionReply), and the decision rides on the message.
async function decideAction(id, approve) {
  const card = document.getElementById(`ask-card-${id}`);
  const kind = (card && card.querySelector('.pill:not(.needs)') || {}).textContent || 'action';
  if (approve && !confirm(`Approve · ${kind}?`)) return;
  card && card.querySelectorAll('button').forEach(b => b.disabled = true);
  try {
    await withDecider(() => post(`/actions/${id}/${approve ? 'approve' : 'deny'}?via=web`, {}));
    toast(approve ? 'approved' : 'denied');
    render(); refreshBadges();
  } catch (e) {
    toast(e.message);
    card && card.querySelectorAll('button').forEach(b => b.disabled = false);
  }
}

async function resolveAskGlobal(id, state) {
  return act(() => post(`/asks/${id}/resolve`, { state }), 'ask ' + state, render);
}

// ================= Respond (prompts engine phase 3, 2026-08-27) =================
// One control for the one act. A response to a card has three parts and used
// to be three buttons that could each only do one of them: an OUTCOME (their
// claim — the card leaves the board the moment they say it, even when the words
// are aimed at tomorrow morning), WORDS, and a DESTINATION (which session, and
// when). It posts ONE `prompts` row; the reference travels as data
// (`in_reply_to`), so the agent is never handed a sentence the hub composed and
// then re-read to work out what they meant.
//
// It is answered FROM THE SESSION, in the ordinary composer. The first cut
// was a <dialog> with its own text box — too thin, it covered the new-session
// bar, and it could not send figures. So Respond arms the
// session's composer (views/threads.js armReply): on a card inside the chat
// it arms it in place; on the Your-turn stack it opens the session at that
// card with the composer armed. The composer's Attach / paste / drop, send-to
// and when are the whole form. `outcome` is the card button's pick — each of
// the hub's words is a button (2026-09-18); undefined = the hub's default.
async function openRespond(id, outcome) {
  try {
    const a = await get('/asks/' + id);
    if (typeof chatState === 'object' && chatState.id === a.thread_id && document.getElementById(cdom('chat') + '-draft')) {
      armReply(a, outcome);
      return;
    }
    armedReply = { thread_id: a.thread_id, re: replyOf(a, outcome) };
    location.hash = `#/sessions/${a.thread_id}/${a.id}`;
  } catch (e) { toast(e.message); }
}

// An error card is not work the owner does — it is a run that wants starting
// again. One line, and the only button that means anything.
async function retryAsk(id) {
  return act(() => post(`/asks/${id}/retry`), 'restarted', () => { render(); refreshBadges(); });
}

// ---- Chat about this ----
// The desktop's right-click on a component (ChatAbout.swift,
// 2026-08-23): talk to a session about THAT thing, with a crop of it and the
// words it is showing handed over. Inside a session it asks that session;
// anywhere else it starts a new one (threads.js newSession/createSession read
// pendingAbout). A right-click in a text box, on a link or with Shift held
// keeps the browser's own menu.
const ABOUT_PICK = '[data-chat], .chat-row,.card, .plot, table';
let pendingAbout = null, newAbout = null;

function aboutSubject(el) {
  // A heading beats the picked tab of a segmented card ("Net worth"), which
  // beats the first bold number.
  const head = el.dataset.chat ? null
    : ['h1, h2, h3, h4', '.title, .card-title', '.seg .on, button.on, .tab.on', 'strong, th'].map(s => el.querySelector(s)).find(Boolean);
  const title = (el.dataset.chat || (head && head.textContent) || el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 80);
  const facts = (el.innerText || '').replace(/\n{3,}/g, '\n\n').trim().slice(0, 1500);
  const sel = window.getSelection();
  const said = String(sel || '').trim();
  const quote = said && el.contains(sel.anchorNode) ? said.slice(0, 500) : '';
  return { title, facts, quote, screen: placeNow().label, shot: false };
}

// Same words as chatPreamble/chatPrompt in ChatAbout.swift: his question
// first (the first line titles the session), the component after it.
function aboutPrompt(a, question, place) {
  const q = (question || '').trim();
  let s = `About “${a.title}” on ${a.screen}: ` + (q || 'explain it — what it is, where each number comes from, whether it is actually right (and fix it if it is not).');
  s += '\n\n' + (a.shot
    ? '[He right-clicked this component in the life web console and chose "Chat about this". The attached image is that component, cropped from his screen — not the whole page.]\n'
    : '[He right-clicked this component in the life web console and chose "Chat about this". No picture; it is described below.]\n');
  s += `Screen: ${a.screen}\nComponent: ${a.title}\n`;
  if (place) s += `URL: ${place.url}\nDrawn by: ${place.files.join(' · ')}\n`;
  if (a.quote) s += `Text he had selected: "${a.quote}"\n`;
  if (a.facts) s += `What it was showing him:\n${a.facts}\n`;
  return s;
}

function aboutTagHTML(a) {
  if (!a) return '';
  return `<div class="about-tag small"><span class="muted">About</span> <strong>${esc(a.title)}</strong> <span class="muted">on ${esc(a.screen)}</span>
    <button class="sm ghost" onclick="dropAbout()" title="Not about it any more">×</button></div>`;
}

function dropAbout() {
  chatState.about = null; newAbout = null;
  document.querySelectorAll('.about-tag').forEach(n => n.remove());
}

// The chat composer's words, wrapped when a component is armed; the armed
// component is spent by the send.
function aboutWrap(text) {
  const a = chatState.about;
  if (!a) return text;
  dropAbout();
  return aboutPrompt(a, text, null);
}

function closeAboutMenu() { const m = document.getElementById('about-menu'); if (m) m.remove(); }

document.addEventListener('contextmenu', e => {
  closeAboutMenu();
  if (e.shiftKey || e.target.closest('input, textarea, select, a[href], .composer, .composer-wrap, nav')) return;
  const view = document.getElementById('view');
  const el = e.target.closest(ABOUT_PICK);
  if (!view || !el || !view.contains(el)) return;
  e.preventDefault();
  const msgs = document.getElementById('msgs');
  const inSession = !!(chatState.id && msgs && msgs.contains(el));
  const m = document.createElement('div');
  m.id = 'about-menu';
  m.className = 'about-menu';
  m.style.left = Math.min(e.clientX, innerWidth - 220) + 'px';
  m.style.top = Math.min(e.clientY, innerHeight - 50) + 'px';
  m.innerHTML = `<button>${inSession ? 'Ask this session about it' : 'Chat about this'}</button>`;
  m.querySelector('button').onclick = () => { closeAboutMenu(); chatAbout(el, inSession); };
  document.body.appendChild(m);
});
document.addEventListener('click', e => { if (!e.target.closest('#about-menu')) closeAboutMenu(); });
document.addEventListener('keydown', e => { if (e.key === 'Escape') closeAboutMenu(); });
window.addEventListener('scroll', closeAboutMenu, true);

async function chatAbout(el, inSession) {
  const a = aboutSubject(el);
  const shot = snapPage(el.getBoundingClientRect()).catch(err => { console.warn('component snap failed', err); return null; });
  if (inSession) {
    chatState.about = a;
    const wrap = document.querySelector('#chat .composer-wrap');
    if (wrap) { wrap.querySelectorAll('.about-tag').forEach(n => n.remove()); wrap.insertAdjacentHTML('afterbegin', aboutTagHTML(a)); }
    const box = document.getElementById('c-chat-draft');
    if (box) box.focus();
    const f = await shot;
    if (f && chatState.about === a) { a.shot = true; uploadFiles([f], 'chat'); }
    return;
  }
  pendingAbout = a;
  pendingPlace = placeNow();
  pendingShot = shot.then(f => { if (f) a.shot = true; return f; });
  const here = location.hash.replace(/^#\/?/, '').replace(/\/+$/, '');
  if (here === 'sessions' || here === '') await newSession();
  else location.hash = '#/sessions';
}

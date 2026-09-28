// The studio layout (skin.css header), part two: no emojis, on every page.
//
// The marks are baked into the shared renderers (ui.js askCard marks,
// threads.js reply lines, attachments), so rather than fork them this strips
// the UI's own emojis from every text node the page draws, as it draws them.
// Nothing is lost: what a mark said (read / approval / rec) is the card's
// tint rail in the skin. Only this fixed set goes — an emoji in something the
// owner or a session wrote is left alone — and the typographic marks
// (✓ ✕ ⌘ ↩ ✻) stay, they are state, not decoration.
(() => {
  const UI = /(?:🔵|🔴|💡|📲|📅|📎|⏰|💬|⏳|⚠︎|⚠|⚙️?)\s?/gu;
  const clean = root => {
    // "sent by you" under the owner's own message says nothing; another
    // sender's line stays.
    if (root.querySelectorAll) for (const by of root.querySelectorAll('.msg .meta .by')) by.classList.toggle('me', by.textContent.trim() === 'sent by you');
    const walk = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    for (let n = walk.nextNode(); n; n = walk.nextNode()) {
      if (n.parentElement && n.parentElement.closest('.bubble .md, .md, textarea, input')) continue;
      UI.lastIndex = 0;
      if (UI.test(n.nodeValue)) n.nodeValue = n.nodeValue.replace(UI, '');
    }
  };
  const start = () => {
    clean(document.body);
    new MutationObserver(ms => {
      for (const m of ms) {
        if (m.type === 'characterData') clean(m.target.parentNode || document.body);
        else for (const a of m.addedNodes) if (a.nodeType === 1) clean(a); else if (a.nodeType === 3 && a.parentNode) clean(a.parentNode);
      }
    }).observe(document.body, { childList: true, subtree: true, characterData: true });
  };
  if (document.body) start(); else document.addEventListener('DOMContentLoaded', start);
})();

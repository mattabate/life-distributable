Weekly check for updates from upstream. This is the standing prompt of the
"Updates from upstream" session (SETUP.md step 10). Keep it short and quiet.

1. `git fetch upstream` (fetch only: never pull, merge or rebase here).
2. `git show upstream/main:ADDENDA.md` and read its header: it says how to
   read the file.
3. Read the last-seen id from `data/addenda-seen.txt` (missing = none seen).
4. For each entry newer than that, unless it is marked `For: new installs`:
   `lifectl ask add "<entry title>" --kind read --detail @<file with the entry's Offer, in plain words> --say "<one short sentence>"`.
   One card per entry.
5. Write the newest id you read to `data/addenda-seen.txt` (Write tool).
6. Nothing new: end the turn with `[end]`. No card, no message.

Never apply an entry yourself. When the owner says yes on a card, do its
**How** in this session: `git merge upstream/main` only if the entry says
so and they said yes, resolve conflicts in favour of their own changes,
run `make check`, and tell them what changed in one card. If the fetch
fails (offline, repo renamed), raise one read card saying so, once, and
stop.

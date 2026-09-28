# Sourced by ops/ota.sh, ops/install-phone.sh and ops/screens.sh (not run).
# Reads the app's identity from app/local.xcconfig (setup writes it,
# git-ignored), falling back to the defaults in app/Identity.xcconfig — the
# same two files the Xcode build reads, so scripts and build always agree.
# Sets LIFE_BUNDLE_ID, LIFE_APP_GROUP, LIFE_TEAM_ID; and sources ops/app.env
# (from ops/app.env.example: the phone's device id) when it exists.
_APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../app" && pwd)"
_xcval() { # $1 = key; local.xcconfig first, then the defaults
  local f v
  for f in "$_APP_DIR/local.xcconfig" "$_APP_DIR/Identity.xcconfig"; do
    [ -f "$f" ] || continue
    v=$(sed -n "s/^[[:space:]]*$1[[:space:]]*=[[:space:]]*//p" "$f" | tail -1 | sed 's/[[:space:]]*$//')
    if [ -n "$v" ]; then echo "$v"; return; fi
  done
}
LIFE_BUNDLE_ID=$(_xcval LIFE_BUNDLE_ID)
LIFE_APP_GROUP=$(_xcval LIFE_APP_GROUP)
LIFE_TEAM_ID=$(_xcval DEVELOPMENT_TEAM)
_APP_ENV="$(dirname "${BASH_SOURCE[0]}")/app.env"
# shellcheck disable=SC1090
[ -f "$_APP_ENV" ] && source "$_APP_ENV"
unset _APP_ENV

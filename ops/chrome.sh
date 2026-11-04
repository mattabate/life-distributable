#!/usr/bin/env bash
# Headless Google Chrome for the ops scripts (web-smoke): `ops/chrome.sh <chrome args…>`.
#
# With session_user set, a session runs as that user under sudo, outside any
# login of its own, and Chrome as that user dies at once ("FATAL path_service
# Failed to get the path for 1001": CoreFoundation cannot give it a home),
# then hangs every page load on a login keychain that user does not have.
# CFFIXED_USER_HOME hands it a throwaway home; --use-mock-keychain keeps it off
# the keychain. browse.js does the same inline.
set -uo pipefail
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
home=$(mktemp -d "${TMPDIR:-/tmp}/life-chrome.XXXXXX") || exit 2
CFFIXED_USER_HOME=$home "$CHROME" --use-mock-keychain "$@"
rc=$?
rm -rf "$home"
exit $rc

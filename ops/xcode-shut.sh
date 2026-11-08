#!/usr/bin/env bash
# Before an xcodebuild: no one else may write into the Xcode project.
#
#   bash ops/xcode-shut.sh app/Life.xcodeproj [more.xcodeproj …]
#
# Xcode makes project.xcworkspace/xcshareddata/swiftpm/configuration mode 777.
# With session_user set, a session that can write xcshareddata can leave a
# workspace scheme whose build pre-action is a shell line, and the owner's
# next build (make check, make mac-build, install-mac.sh, install-phone.sh,
# ota.sh, app-ui.sh) runs it as the owner. So: take group/other write off
# everything in the project, then refuse a build when any file in it is owned
# by someone else (a plant left before the chmod). Xcode leaves an existing
# folder's mode alone, so this holds between runs.
set -euo pipefail
me="$(id -un)"
if [ "${1:-}" = --test ]; then
  t="$(mktemp -d)"; trap 'rm -rf "$t"' EXIT
  d="$t/L.xcodeproj/project.xcworkspace/xcshareddata/swiftpm/configuration"
  mkdir -p "$d"; chmod 777 "$d" "$t/L.xcodeproj/project.xcworkspace/xcshareddata"
  bash "$0" "$t/L.xcodeproj"
  [ -z "$(find "$t/L.xcodeproj" -perm -0020 -o -perm -0002)" ] || { echo "xcode-shut: FAILED, still writable" >&2; exit 1; }
  ! bash "$0" "$t/none.xcodeproj" 2>/dev/null || { echo "xcode-shut: FAILED, a missing project passed" >&2; exit 1; }
  echo "xcode-shut: OK"; exit 0
fi
for p in "$@"; do
  [ -d "$p" ] || { echo "xcode-shut: no project $p" >&2; exit 1; }
  chmod -R go-w "$p"
  other="$(find "$p" ! -user "$me" -print -quit)"
  [ -z "$other" ] || { echo "xcode-shut: $other is not $me's; no build (remove it, then rerun)" >&2; exit 1; }
done

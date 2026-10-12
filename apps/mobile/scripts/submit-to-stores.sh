#!/usr/bin/env bash
# Submit an RFSPager release that CI has already built and uploaded
# (TestFlight + Play internal) to the public stores:
#   iOS      App Store review, automatic release once approved
#   Android  internal -> production, completed rollout to everyone
#
# Usage: apps/mobile/scripts/submit-to-stores.sh <version> [ios|android|both] [revision]
#   e.g. apps/mobile/scripts/submit-to-stores.sh 5.4.0
#
# Needs: fastlane, gh (signed in to The-IT-Dept/rfspager), ssh to apps01,
# and the release notes at apps/mobile/fastlane/release-notes/<version>.txt.
# Credentials are never stored in the repo:
#   ASC_KEY_PATH        App Store Connect API key (.p8). Default: the
#                       4R8T7V2Z9H key in ~/code/boringapps/.secrets
#   PLAY_JSON_KEY_PATH  Play service-account key. Default: copied from the
#                       Go server (apps01:/opt/rfspager-go/google-play.json)
#                       into a private temp dir that is deleted on exit.
# See docs/mobile-releases.md.
set -euo pipefail

VERSION="${1:?usage: submit-to-stores.sh <version> [ios|android|both] [revision]}"
PLATFORM="${2:-both}"
REVISION="${3:-0}"
REPO="The-IT-Dept/rfspager"
PROD_HOST="${PROD_HOST:-root@100.64.244.122}"

[[ "$VERSION" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]] || { echo "version must be MAJOR.MINOR.PATCH" >&2; exit 1; }
case "$PLATFORM" in ios|android|both) ;; *) echo "platform must be ios, android or both" >&2; exit 1 ;; esac

# Same formula as mobile-release.yml: one store build number per version.
BUILD_NUMBER=$(( 500000000 + BASH_REMATCH[1] * 10000000 + BASH_REMATCH[2] * 100000 + BASH_REMATCH[3] * 1000 + REVISION ))
MOBILE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
NOTES="$MOBILE_DIR/fastlane/release-notes/$VERSION.txt"
[ -s "$NOTES" ] || { echo "missing release notes: $NOTES" >&2; exit 1; }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/rfspager-stores.XXXXXX")"
chmod 700 "$WORK"
trap 'rm -rf "$WORK"' EXIT

export APP_VERSION="$VERSION" BUILD_NUMBER RELEASE_NOTES_PATH="$NOTES"
# Keep fastlane's output to the release itself.
export FASTLANE_SKIP_UPDATE_CHECK=1 FASTLANE_HIDE_CHANGELOG=1 FASTLANE_HIDE_TIMESTAMP=1
echo "Submitting $VERSION (build $BUILD_NUMBER) to: $PLATFORM"
echo "Notes ($(wc -c < "$NOTES" | tr -d ' ') chars): $(cat "$NOTES")"

if [ "$PLATFORM" != "android" ]; then
  export ASC_KEY_ID="${ASC_KEY_ID:-4R8T7V2Z9H}"
  export ASC_ISSUER_ID="${ASC_ISSUER_ID:-69a6de73-b975-47e3-e053-5b8c7c11a4d1}"
  export ASC_KEY_PATH="${ASC_KEY_PATH:-$HOME/code/boringapps/.secrets/AuthKey_4R8T7V2Z9H.p8}"
  [ -r "$ASC_KEY_PATH" ] || { echo "App Store Connect key not readable: $ASC_KEY_PATH" >&2; exit 1; }
  (cd "$MOBILE_DIR" && fastlane ios submit_for_review)
fi

if [ "$PLATFORM" != "ios" ]; then
  # Never promote a bundle Play would reject or that crashes on x86 devices:
  # check the exact AAB CI uploaded for x86_64 libraries and 16 KB alignment.
  # The artifact name carries the tag, so it identifies the exact run.
  RUN_ID="$(gh api "repos/$REPO/actions/artifacts?name=android-aab-v$VERSION" \
    -q '[.artifacts[] | select(.expired | not)][0].workflow_run.id')"
  [ -n "$RUN_ID" ] || { echo "no android-aab-v$VERSION artifact: has mobile-release finished for this tag?" >&2; exit 1; }
  gh run download "$RUN_ID" -R "$REPO" -n "android-aab-v$VERSION" -D "$WORK/aab"
  AAB="$WORK/aab/app-release.aab"
  # Read the listing once: piping unzip into `grep -q` under pipefail fails
  # the pipeline when grep exits early, even on a match.
  LIBS="$(unzip -l "$AAB" | grep -oE "base/lib/[^/]+/" | sort | uniq -c)"
  echo "AAB native libraries:"; echo "$LIBS"
  [[ "$LIBS" == *"base/lib/x86_64/"* ]] || { echo "STOP: no x86_64 libraries in the AAB" >&2; exit 1; }
  python3 -I "$MOBILE_DIR/scripts/check-16kb-alignment.py" "$AAB" || { echo "STOP: 16 KB alignment check failed" >&2; exit 1; }

  if [ -z "${PLAY_JSON_KEY_PATH:-}" ]; then
    export PLAY_JSON_KEY_PATH="$WORK/play.json"
    (umask 077 && ssh -o BatchMode=yes "$PROD_HOST" cat /opt/rfspager-go/google-play.json > "$PLAY_JSON_KEY_PATH")
  fi
  (cd "$MOBILE_DIR" && fastlane android promote_to_production)
fi

echo "Done. Check state with: docs/mobile-releases.md (Checking store state)"

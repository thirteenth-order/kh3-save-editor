#!/bin/sh
# End-to-end check of the container image, against a synthetic save.
#
# The image can fail in ways the Go tests cannot see: a mount whose account id
# is no longer derivable from the path, a read-only root filesystem that turns
# out not to be enough, a published port that the UI's own Host check refuses,
# a URL printed for an address nothing can connect to. So this runs the real
# image, with the hardening flags the README recommends, and asserts on what
# comes back.
#
#   tools/docker-smoke.sh [image]
#
# Needs a fixture at $KH3_FIXTURE (default /tmp/kh3-fixture), as written by
# `make fixture`. Never point it at a real save: it edits what it is given.

set -eu

IMAGE=${1:-kh3save:local}
FIXTURE=${KH3_FIXTURE:-/tmp/kh3-fixture}
SAVES="$FIXTURE/Documents/KINGDOM HEARTS III"
# Host and container port must match: the UI checks the Host header, port
# included, so a container published on some other port refuses its own URL.
PORT=${KH3_SMOKE_PORT:-18787}
NAME=kh3save-smoke-$$

HARDEN="--read-only --cap-drop ALL --security-opt no-new-privileges"
HARDEN="$HARDEN --user $(id -u):$(id -g)"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok()   { echo "ok    $*"; }

cleanup() { docker rm -f "$NAME" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

[ -d "$SAVES" ] || fail "no fixture at $SAVES (run: make fixture)"

# --------------------------------------------------------------------- CLI --
# --network none is the real claim being tested. Every one of these commands
# is offline by design, so the container is given no network at all and has to
# work anyway.
run() {
  # shellcheck disable=SC2086
  docker run --rm --network none $HARDEN -v "$SAVES:/saves" "$IMAGE" "$@"
}

run version >/dev/null || fail "version"
ok "runs with no network, no capabilities, read-only root"

out=$(run info /saves) || fail "info exited nonzero"
for slot in KHIII_slot0.bin KHIII_slot1.bin KHIII_slot2.bin; do
  echo "$out" | grep -q "$slot" || fail "info did not report $slot"
done
# The account id is a directory name on the path to the save. If the mount
# point ever stops preserving it, this is what notices.
echo "$out" | grep -q "difficulty   3 (Critical)" || fail "info misread a difficulty"
ok "info reads every slot through the mount"

run verify /saves >/dev/null || fail "verify"
ok "verify passes on an untouched fixture"

# A write, then proof it was a real one: the backup exists on the host side,
# and the rewritten save still passes both integrity fields.
before=$(ls "$SAVES/Steam"/*/SaveGames/kh3sv2/data | wc -l)
run swap /saves -d Proud >/dev/null || fail "swap"
after=$(ls "$SAVES/Steam"/*/SaveGames/kh3sv2/data | wc -l)
[ "$after" -gt "$before" ] || fail "swap wrote no backup"
run verify /saves >/dev/null || fail "verify after swap"
out=$(run info /saves)
echo "$out" | grep -q "difficulty   2 (Proud)" || fail "swap did not take"
ok "swap writes through the mount, leaves a backup, and verifies"

# Files land on the host owned by the caller, not by the image's uid. Getting
# this wrong is the classic bind-mount trap and it is invisible in-container.
owner=$(find "$SAVES" -name 'KHIII_slot0.bin*' -newer "$SAVES" -printf '%U\n' 2>/dev/null | head -1)
[ -z "$owner" ] || [ "$owner" = "$(id -u)" ] || fail "wrote files owned by uid $owner, not $(id -u)"
ok "written files belong to the calling user"

# --------------------------------------------------------------------- GUI --
# shellcheck disable=SC2086
docker run -d --name "$NAME" -p "127.0.0.1:$PORT:$PORT" $HARDEN \
  -e "KH3_ADDR=0.0.0.0:$PORT" --tmpfs /config:mode=1777,size=1m \
  -v "$SAVES:/saves" "$IMAGE" gui -no-browser >/dev/null || fail "gui did not start"

url=""
i=0
while [ "$i" -lt 50 ]; do
  url=$(docker logs "$NAME" 2>&1 | sed -n 's|^kh3save UI: \(http://[^ ]*\)$|\1|p' | head -1)
  [ -n "$url" ] && break
  i=$((i + 1))
  sleep 0.2
done
[ -n "$url" ] || fail "gui never printed a URL: $(docker logs "$NAME" 2>&1 | head -5)"

token=${url##*t=}
base="http://127.0.0.1:$PORT"
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

# The URL the container prints has to be one the host can open. A bind to
# 0.0.0.0 is reported by Go as [::], and publishing it is IPv4, so printing
# the address verbatim would hand out a link that does not connect.
case "$url" in
  *"127.0.0.1:$PORT"*) ;;
  *) fail "gui printed an address that cannot be opened: $url" ;;
esac
ok "gui prints a connectable URL"

[ "$(code "$base/?t=$token")" = 200 ] || fail "gui refused a request with a good token"
ok "gui serves the page through the published port"

# Publishing the port must not have cost any of the remaining gates.
[ "$(code "$base/")" = 403 ] || fail "gate 2: a request with no token was served"
ok "gate 2 holds: no token, no answer"

[ "$(code -H "Host: evil.example.com:$PORT" "$base/?t=$token")" = 403 ] \
  || fail "gate 3: a rebound hostname was served"
ok "gate 3 holds: a rebound hostname is refused"

[ "$(code -H "Sec-Fetch-Site: cross-site" "$base/api/scan?t=$token")" = 403 ] \
  || fail "gate 4: a cross-site request was served"
ok "gate 4 holds: a cross-site request is refused"

# A container published on a mismatched port is refused, and says why: that
# combination is easy to type and impossible to diagnose from "bad host".
docker rm -f "$NAME" >/dev/null 2>&1
# shellcheck disable=SC2086
docker run -d --name "$NAME" -p "127.0.0.1:$((PORT + 1)):$PORT" $HARDEN \
  -e "KH3_ADDR=0.0.0.0:$PORT" --tmpfs /config:mode=1777,size=1m \
  -v "$SAVES:/saves" "$IMAGE" gui -no-browser >/dev/null || fail "gui did not restart"
i=0
while [ "$i" -lt 50 ]; do
  docker logs "$NAME" 2>&1 | grep -q 'kh3save UI' && break
  i=$((i + 1)); sleep 0.2
done
body=$(curl -s "http://127.0.0.1:$((PORT + 1))/?t=$token" || true)
echo "$body" | grep -q "publish it on the same port" \
  || fail "a mismatched port was not explained: $body"
ok "a mismatched published port explains itself"

docker rm -f "$NAME" >/dev/null 2>&1
# shellcheck disable=SC2086
docker run -d --name "$NAME" -p "127.0.0.1:$PORT:$PORT" $HARDEN \
  -e "KH3_ADDR=0.0.0.0:$PORT" --tmpfs /config:mode=1777,size=1m \
  -v "$SAVES:/saves" "$IMAGE" gui -no-browser >/dev/null || fail "gui did not restart"
i=0
while [ "$i" -lt 50 ]; do
  token=$(docker logs "$NAME" 2>&1 | sed -n 's|.*?t=\([a-f0-9]*\)$|\1|p' | head -1)
  [ -n "$token" ] && break
  i=$((i + 1)); sleep 0.2
done
[ -n "$token" ] || fail "gui did not restart cleanly"

# The mount is autodetected, which is the whole point of the symlink the image
# puts in $HOME: the UI lists a save the moment it opens, with nothing typed.
curl -s "$base/api/scan?t=$token" | grep -q '"slots"' \
  || fail "gui autodetected nothing at /saves"
ok "gui autodetects the mounted save folder"

echo
echo "==> $IMAGE is good"

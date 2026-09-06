#!/bin/sh
# Re-shoot the README screenshots of the browser interface, headless.
#
#   tools/demo/shoot.sh              # every shot
#   tools/demo/shoot.sh saves fields # just these
#
# Companion to record.sh, which does the same job for the terminal GIFs, and it
# exists for the same reason: a screenshot taken by hand is a screenshot nobody
# can reproduce, and it goes stale the moment the interface moves. The one that
# shipped before this script showed a front page that no longer exists.
#
# Everything shown is the real binary serving a real save. What is synthetic is
# the save: a fresh tree from tools/genfixture under account 76561190000000000,
# an id that belongs to nobody. A real save embeds its owner's SteamID64 and
# their whole playthrough, and an image renders text that no grep will ever
# find, so no shot may touch one. HOME points into the throwaway tree, which is
# also what makes the interface discover the fixture as if it were installed.
#
# Needs a Chromium and a Go toolchain.

set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OUT=${OUT:-$ROOT/docs}
WORK=${WORK:-/tmp/kh3-shots}
PORT=${PORT:-18973}

# Retina-ish, because these are displayed at 720px wide in the README and a
# 1x shot of a 900px page looks soft next to the terminal GIFs.
WIDTH=${WIDTH:-1180}
SCALE=${SCALE:-2}

chrome=""
for c in chromium chromium-browser google-chrome-stable google-chrome; do
	if command -v "$c" >/dev/null 2>&1; then chrome=$c; break; fi
done
[ -n "$chrome" ] || {
	echo "missing: a Chromium (chromium, or google-chrome)" >&2
	exit 1
}
command -v go >/dev/null 2>&1 || {
	echo "missing: go (needed to build the binary and the fixture)" >&2
	exit 1
}

SAVE_DIR="Documents/KINGDOM HEARTS III/Steam/76561190000000000/SaveGames/kh3sv2/data"

echo "==> building kh3save"
mkdir -p "$WORK/bin" "$OUT"
CGO_ENABLED=0 go build -trimpath -o "$WORK/bin/kh3save" "$ROOT/cmd/kh3save"

# Full-size saves: the dashboard reads the record block at the tail, and a
# short fixture stops before it and would be shot saying so.
echo "==> building the fixture"
rm -rf "$WORK/home"
go run "$ROOT/tools/genfixture" -layout -full "$WORK/home" >/dev/null

echo "==> serving on 127.0.0.1:$PORT"
HOME=$WORK/home "$WORK/bin/kh3save" gui -no-browser -addr "127.0.0.1:$PORT" \
	>"$WORK/server.log" 2>&1 &
server=$!
trap 'kill $server 2>/dev/null || true' EXIT INT TERM

# Wait for the URL line itself, not merely for the log to stop being empty:
# the port is fixed but the token is minted at startup, and that line is the
# only place it ever appears. Waiting on "not empty" races a half-written line
# and a startup warning that arrives first, both of which read as "up" and
# then yield no URL.
URL=""
i=0
while [ -z "$URL" ]; do
	URL=$(sed -n 's|^kh3save UI: \(http[^ ]*\).*|\1|p' "$WORK/server.log" 2>/dev/null | head -1)
	[ -n "$URL" ] && break
	kill -0 "$server" 2>/dev/null || {
		echo "the server exited before it was ready:" >&2
		cat "$WORK/server.log" >&2
		exit 1
	}
	i=$((i + 1))
	[ "$i" -lt 60 ] || {
		echo "the server never announced itself; see $WORK/server.log" >&2
		exit 1
	}
	sleep 0.5
done

# --------------------------------------------------------------- the shots --
# Chromium's screenshot mode cannot click, so each shot names the state it
# wants in the fragment and the page routes itself there on load. That is the
# interface's own router, not a back door opened for this script: the same
# fragment typed into the address bar does the same thing, which is what makes
# a save linkable and a reload survivable.
shot() {
	name=$1
	height=$2
	echo "==> $name"
	# --force-prefers-reduced-motion is load-bearing, not politeness. Cards and
	# panels enter with `animation: rise ... both`, whose from-state is opacity
	# zero, so a capture that lands before the animation settles photographs an
	# empty page with the whole DOM present and invisible. The page's own
	# reduced-motion rule collapses those animations to nothing, which makes a
	# shot show the settled state every time instead of most times.
	"$chrome" --headless --disable-gpu --hide-scrollbars --no-sandbox \
		--force-prefers-reduced-motion \
		--force-device-scale-factor="$SCALE" \
		--window-size="$WIDTH,$height" \
		--virtual-time-budget=12000 \
		--screenshot="$OUT/screenshot-$name.png" \
		--user-data-dir="$WORK/chrome" \
		"$URL$3" >/dev/null 2>&1
	[ -s "$OUT/screenshot-$name.png" ] || {
		echo "  produced nothing" >&2
		exit 1
	}
}

# The summary shot lifts both spoiler covers on purpose: it is the one image
# whose job is to show what the dashboard holds, and a README picture of two
# covers would be useless. The interface itself still comes up covered.
want=${*:-saves summary fields json}
for name in $want; do
	case $name in
	saves) shot saves 1010 "" ;;
	summary) shot summary 1800 "#slot=0&tab=overview&reveal=party,story_flags" ;;
	# Down to the accessory slots, because the type-byte picker is the clearest
	# thing the schema-driven form does and it is four folds deep.
	fields) shot fields 1830 \
		"#slot=0&tab=edit&shut=header&reveal=characters&open=characters,characters/Sora,characters/Sora/equipment,characters/Sora/equipment/accessories" ;;
	json) shot json 1320 "#slot=0&tab=json" ;;
	# Not in the default set: it is not a feature to advertise, and it changes
	# only when the notices do. `tools/demo/shoot.sh legal` when they do.
	legal) shot legal 1900 "#legal" ;;
	*)
		echo "no such shot: $name" >&2
		exit 1
		;;
	esac
done

echo
echo "==> written to $OUT"

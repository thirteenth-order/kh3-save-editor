#!/bin/sh
# Record the README demo GIFs with asciinema, render them with agg.
#
#   tools/demo/record.sh              # every scene
#   tools/demo/record.sh swap info    # just these
#
# Everything a scene shows is the real binary run against a real save. Nothing
# is mocked up. What is synthetic is the save itself: each scene gets a fresh
# fixture tree from tools/genfixture, keyed to account 76561190000000000, an
# id that belongs to nobody. A real save embeds its owner's SteamID64 and their
# whole playthrough, so no recording may ever touch one.
#
# Three things keep a recording clean of the machine that made it:
#
#   1. The scene runs under `env -i`, so it inherits no USER, HOSTNAME, PWD,
#      LOGNAME, SSH_*, or anything else from the caller. HOME points into the
#      throwaway fixture.
#   2. The prompt is drawn by tools/demo/lib.sh from a literal string, not from
#      $PS1, and the elided label it prints holds no path component that could
#      identify anyone.
#   3. The asciicast header, which normally records the wall-clock time and the
#      captured environment, is rewritten to a fixed one before rendering.
#
# Needs asciinema and agg (https://docs.asciinema.org/manual/agg/).

set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
DEMO=$ROOT/tools/demo
OUT=${OUT:-$ROOT/media}
WORK=${WORK:-/tmp/kh3-demo}

# Prefers JetBrains Mono where it is installed, which is also agg's own first
# choice, and falls back to a font every distribution ships.
FONT=${FONT:-'JetBrains Mono,DejaVu Sans Mono,Liberation Mono'}
FONT_SIZE=${FONT_SIZE:-16}
THEME=${THEME:-1c1b1a,d8d4cd,1c1b1a,d97757,9fb37a,dfba73,7fa5c4,b48ead,89b8b0,d8d4cd}
SPEED=${SPEED:-1}

need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "missing: $1  ($2)" >&2
		exit 1
	}
}
need asciinema "pip install asciinema, or your package manager"
need agg "cargo install --git https://github.com/asciinema/agg"
need go "needed to build the binary and the fixture"

# --------------------------------------------------------------- the binary --
# Built into the work tree rather than bin/, so a recording never depends on
# whatever happens to be sitting in bin/ already, and so the scenes can call
# `kh3save` as a bare word the way a reader would.
echo "==> building kh3save"
mkdir -p "$WORK/bin"
CGO_ENABLED=0 go build -trimpath -o "$WORK/bin/kh3save" "$ROOT/cmd/kh3save"

# --------------------------------------------------------------- the fixture --
# Rebuilt from scratch for every scene: swap and patch edit what they are given
# and leave a .bak behind, so a scene recorded second would otherwise open on
# the leftovers of the first.
DOCS_DIR="Documents"
SAVE_DIR="$DOCS_DIR/KINGDOM HEARTS III/Steam/76561190000000000/SaveGames/kh3sv2/data"

# A short fixture is 0x20000 bytes, which is a valid save and is what the
# golden vectors were built on, and it stops long before the record block at
# the tail. A scene that wants to show the tail asks for `fixture: full` and
# gets saves the size of a real one -- 9.3 MB each, and still under a second to
# build and zip, so the only reason not to make it the default is that the
# short one is what the rest of the test suite uses.
fixture() {
	rm -rf "$WORK/home"
	case ${1:-short} in
	full) go run "$ROOT/tools/genfixture" -layout -full "$WORK/home" >/dev/null ;;
	*) go run "$ROOT/tools/genfixture" -layout "$WORK/home" >/dev/null ;;
	esac

	# A partial patch document, so the scene can cat it rather than type it.
	# One key from three different regions, including the record block at the
	# tail, which only a full-size save reaches -- the json scene asks for one.
	cat >"$WORK/home/$SAVE_DIR/tweak.json" <<'JSON'
{
  "header": { "munny": 65535, "map_path": "/Game/Levels/ca/ca_01/ca_01" },
  "characters": { "Sora": { "hp": 99, "mp": 99 } },
  "records": { "attractions": { "0": { "high_score": 4200 } } }
}
JSON

	# A backup archive of the whole tree, which is what one actually looks like.
	(cd "$WORK/home/$DOCS_DIR" && zip -qr backup.zip "KINGDOM HEARTS III")
}

# ---------------------------------------------------------------- recording --
# A scene declares its own terminal size and starting directory in its header
# comment, because they differ: the archive scene prints paths that carry a
# whole directory tree inside them and needs the columns for it.
geometry() { sed -n 's/.*geometry: \([0-9]*x[0-9]*\).*/\1/p' "$1" | head -1; }
scene_cwd() { sed -n 's/.*cwd: \([a-z]*\).*/\1/p' "$1" | head -1; }
scene_fixture() { sed -n 's/.*fixture: \([a-z]*\).*/\1/p' "$1" | head -1; }

record() {
	name=$1
	scene=$DEMO/scenes/$name.sh
	[ -f "$scene" ] || {
		echo "no such scene: $name" >&2
		exit 1
	}

	geom=$(geometry "$scene")
	cols=${geom%x*}
	rows=${geom#*x}
	case $(scene_cwd "$scene") in
	documents) cwd=$WORK/home/$DOCS_DIR ;;
	*) cwd=$WORK/home/$SAVE_DIR ;;
	esac

	kind=$(scene_fixture "$scene")
	echo "==> $name  (${cols}x${rows}${kind:+, $kind fixture})"
	mkdir -p "$WORK/casts" "$OUT"

	# Preflight. A line wider than the terminal wraps and a scene taller than it
	# scrolls, and both look like a broken recording rather than a broken
	# geometry -- by the time it shows up it is a GIF, not an error. So the
	# scene runs once with the delays turned off and not attached to a terminal,
	# where nothing wraps, and its real dimensions are measured.
	fixture "$kind"
	fit=$(env -i HOME="$WORK/home" PATH="$WORK/bin:/usr/bin:/bin" \
		TERM=dumb LANG=C.UTF-8 TYPE_DELAY=0 BEAT=0 \
		sh -c "cd \"$cwd\" && sh \"$scene\"" 2>&1 |
		sed 's/\x1b\[[0-9;]*m//g')
	w=$(printf '%s\n' "$fit" | wc -L)
	h=$(printf '%s\n' "$fit" | wc -l)
	echo "    fits ${w}x${h} of ${cols}x${rows}"
	[ "$w" -le "$cols" ] || echo "    warning: widest line is $w, terminal is $cols -- it will wrap" >&2
	[ "$h" -lt "$rows" ] || echo "    warning: $h lines in $rows rows -- it will scroll" >&2

	fixture "$kind"

	# env -i is the whole point: the scene starts from an empty environment and
	# is handed back only what it cannot run without. TERM has to be something
	# that admits color, or the prompt records as plain text.
	asciinema rec \
		--quiet --overwrite \
		--cols "$cols" --rows "$rows" \
		--env TERM \
		--title "kh3save $name" \
		-c "env -i HOME=$WORK/home PATH=$WORK/bin:/usr/bin:/bin TERM=xterm-256color LANG=C.UTF-8 sh -c 'cd \"$cwd\" && exec sh \"$scene\"'" \
		"$WORK/casts/$name.cast"

	# Rewrite the header. asciinema stores the recording's wall-clock timestamp
	# and the environment it captured; neither belongs in a committed artifact,
	# and dropping both makes re-recording a no-op in git when nothing changed.
	sed -i "1s|.*|{\"version\": 2, \"width\": $cols, \"height\": $rows, \"title\": \"kh3save $name\", \"env\": {\"TERM\": \"xterm-256color\"}}|" \
		"$WORK/casts/$name.cast"

	agg --font-family "$FONT" --font-size "$FONT_SIZE" \
		--theme "$THEME" --speed "$SPEED" \
		--idle-time-limit 1.2 --fps-cap 24 --last-frame-duration 3 \
		"$WORK/casts/$name.cast" "$OUT/demo-$name.gif"

	echo "    $OUT/demo-$name.gif  ($(du -h "$OUT/demo-$name.gif" | cut -f1))"
}

if [ $# -gt 0 ]; then
	for s in "$@"; do record "$s"; done
else
	for f in "$DEMO"/scenes/*.sh; do
		b=${f##*/}
		record "${b%.sh}"
	done
fi

echo
echo "==> done. casts in $WORK/casts, gifs in $OUT"

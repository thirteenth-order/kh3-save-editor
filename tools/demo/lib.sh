# Sourced by every scene in scenes/. Draws the prompt and types commands out.
#
# The prompt is a literal string assembled here. It is never $PS1 and never the
# real working directory, so a recording cannot pick up a username, a hostname,
# a machine path or a shell theme no matter whose machine it is recorded on.
# The only path a scene may show is one it typed itself, and every scene runs
# inside the synthetic fixture tree, keyed to an account that belongs to
# nobody. See tools/demo/README.md.

# Gold is the accent the README and the UI already use (#dfba73).
GOLD='\033[38;2;223;186;115m'
DIM='\033[38;2;125;125;135m'
OFF='\033[0m'

# The directory label the prompt shows. Deliberately elided: the real path
# holds a home directory and, in a real install, a SteamID64.
DEMO_CWD=${DEMO_CWD:-'…/kh3sv2/data'}

TYPE_DELAY=${TYPE_DELAY:-0.035}
BEAT=${BEAT:-0.45}

prompt() {
	printf "${GOLD}%s${OFF} ${DIM}\$${OFF} " "$DEMO_CWD"
}

# type_out writes a string one character at a time. ${s%"${s#?}"} is the first
# character of $s; the space-safe POSIX way to do this without an array.
type_out() {
	s=$1
	while [ -n "$s" ]; do
		printf '%s' "${s%"${s#?}"}"
		s=${s#?}
		sleep "$TYPE_DELAY"
	done
}

# run types a command at the prompt, pauses as if a key were pressed, then
# runs it for real. Nothing here is faked output: the scenes record the actual
# tool against actual (synthetic) saves.
run() {
	prompt
	type_out "$1"
	sleep "$BEAT"
	printf '\n'
	eval "$1" || true
	sleep "$BEAT"
}

# say prints a dim comment line, for framing a step without narration.
say() {
	prompt
	printf "%b" "$DIM"
	type_out "# $1"
	printf "%b" "$OFF"
	sleep "$BEAT"
	printf '\n'
	sleep 0.2
}

# hold leaves the final frame on screen before the GIF loops around.
hold() {
	prompt
	sleep "${1:-2.4}"
	printf '\n'
}

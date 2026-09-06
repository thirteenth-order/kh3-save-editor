#!/bin/sh
# The whole editing surface: dump renders it, patch takes back the keys you
# changed. One key each from the header, a character and the record block at
# the tail, which is why this one wants a full-size save.
# geometry: 88x26 cwd: data fixture: full
set -u
. "$(dirname "$0")/../lib.sh"

run "kh3save dump KHIII_slot0.bin -o slot0.json"
say "patch takes only the keys you want"
run "cat tweak.json"
run "kh3save patch KHIII_slot0.bin tweak.json"
hold 1.2

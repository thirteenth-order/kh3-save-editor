#!/bin/sh
# Fields the interface does not expose, through dump and patch.
# geometry: 84x20 cwd: data
set -u
. "$(dirname "$0")/../lib.sh"

run "kh3save dump KHIII_slot0.bin -o slot0.json"
say "patch takes only the keys you want"
run "cat tweak.json"
run "kh3save patch KHIII_slot0.bin tweak.json"
hold 1.2

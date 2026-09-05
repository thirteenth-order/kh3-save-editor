#!/bin/sh
# The headline: preview a difficulty change, then commit it. geometry: 84x33 cwd: data
set -u
. "$(dirname "$0")/../lib.sh"

say "preview first: -n writes nothing"
run "kh3save swap KHIII_slot0.bin -d Critical -n"
say "the same command, without -n"
run "kh3save swap KHIII_slot0.bin -d Critical"
hold 1.2

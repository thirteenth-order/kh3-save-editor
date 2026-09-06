#!/bin/sh
# The Steam wrapper comes off and goes back on, byte for byte.
# A full-size fixture, so the byte counts are the ones a real save has.
# geometry: 88x27 cwd: data fixture: full
set -u
. "$(dirname "$0")/../lib.sh"

say "-to plain writes filesize + 0x10, the length a console slot is"
run "kh3save convert KHIII_slot0.bin -to plain -o plain/"
say "no wrapper, so no account id is involved at all"
run "kh3save info plain/KHIII_slot0.bin"
say "going back needs the id: a plain save carries none"
run "kh3save convert plain/KHIII_slot0.bin -to pc -account 76561190000000000"
run "cmp KHIII_slot0.bin converted/KHIII_slot0.bin && echo 'byte for byte'"
hold 1.6

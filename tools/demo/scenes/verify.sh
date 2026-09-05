#!/bin/sh
# Both integrity fields, and what one flipped byte does to them.
# geometry: 84x20 cwd: data
set -u
. "$(dirname "$0")/../lib.sh"

run "kh3save verify ."
say "flip one byte, 4 KiB into slot 2"
run "printf '\\336' | dd of=KHIII_slot2.bin bs=1 seek=4096 conv=notrunc"
run "kh3save verify ."
hold 1.2

#!/bin/sh
# Everything the format layer knows about one save, including the record block
# at the tail. That block only exists on a full-size save, hence the fixture.
# geometry: 84x32 cwd: data fixture: full
set -u
. "$(dirname "$0")/../lib.sh"

run "kh3save info -l KHIII_slot0.bin"
hold 1.6

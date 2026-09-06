#!/bin/sh
# A .zip backup works anywhere a save folder does, unpacked or not.
# The saves inside are addressed <archive>!<member>, the way jar: URLs do it,
# which is also what keeps the account id visible in the path.
# geometry: 92x39 cwd: documents
set -u
. "$(dirname "$0")/../lib.sh"
DEMO_CWD='…/Documents'

say "a backup archive, still zipped"
run "kh3save info backup.zip"
hold 1.2

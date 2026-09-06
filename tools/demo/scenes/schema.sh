#!/bin/sh
# Every choice in the editor is backed by a real table, and this is the list.
# geometry: 84x38 cwd: data
set -u
. "$(dirname "$0")/../lib.sh"

say "every id a save holds is named by one of these"
run "kh3save schema -tables"
hold 1.6

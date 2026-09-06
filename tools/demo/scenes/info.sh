#!/bin/sh
# Read a save folder without touching it. geometry: 84x34 cwd: data
set -u
. "$(dirname "$0")/../lib.sh"

run "kh3save info ."
hold 1.2

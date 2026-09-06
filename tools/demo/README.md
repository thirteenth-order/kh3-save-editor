# demo

Records the terminal GIFs in the README, with
[asciinema](https://docs.asciinema.org/) and
[agg](https://docs.asciinema.org/manual/agg/).

```sh
tools/demo/record.sh              # every scene, into docs/
tools/demo/record.sh swap verify  # just these two
make demo                         # the same thing
```

Each scene writes `docs/demo-<name>.gif` and leaves the asciicast behind in
`/tmp/kh3-demo/casts`, which is worth keeping: a cast is a few kilobytes of
text and can be re-rendered at a different size, theme or font without
recording anything again.

## No real save, ever

A Kingdom Hearts III save embeds its owner's SteamID64 and their entire
playthrough, so a recording may never touch one. Every scene runs against a
throwaway tree from `tools/genfixture`, keyed to `76561190000000000` -- a
valid-looking account id that belongs to nobody -- rebuilt from scratch before
each scene, because `swap` and `patch` edit what they are given and leave a
`.bak` behind.

Three further things keep a recording clean of the machine that made it:

- **The environment is empty.** The scene runs under `env -i` and is handed
  back only `HOME` (pointing into the fixture), `PATH`, `TERM` and `LANG`. No
  `USER`, no `HOSTNAME`, no `PWD`, no `SSH_*`, nothing to leak.
- **The prompt is a literal.** `lib.sh` draws it from a fixed string rather
  than `$PS1`, so no shell theme, host or user can appear in it, and the
  directory label is elided to `…/kh3sv2/data`. Every path a scene shows is one
  it typed itself.
- **The header is rewritten.** An asciicast records the wall-clock time of the
  recording and the environment it captured. `record.sh` replaces the whole
  header line with a fixed one before rendering, which also means re-recording
  an unchanged scene is a no-op in git.

Nothing in a scene is mocked up. The commands are real, the binary is built
from the working tree, and the output is whatever it actually printed.

## Adding a scene

Drop a `scenes/<name>.sh` in, and `record.sh` with no arguments picks it up.

```sh
#!/bin/sh
# One line on what it shows. geometry: 84x20 cwd: data
set -u
. "$(dirname "$0")/../lib.sh"

run "kh3save abilities KHIII_slot0.bin"
hold 1.2
```

`run` types a command out and then runs it; `say` types a `#` comment without
running anything; `hold` leaves the last frame up before the loop comes round.

Three settings live in the header comment, because they differ per scene:

| | |
| :-- | :-- |
| `geometry: WxH` | terminal size, in characters |
| `cwd: data` | start in the save directory (the default) |
| `cwd: documents` | start one level above `KINGDOM HEARTS III`, for the archive scene |
| `fixture: full` | saves the size of a real one, 9.3 MB each |

The default fixture is `0x20000` bytes. That is a structurally valid save and
it is what the golden vectors were built on, but it stops long before the
record block at the tail, so a scene that wants to show minigame bests, the
Flantastic Seven or the album limit asks for `fixture: full`. It costs about a
second per scene and nothing else.

Getting the geometry wrong is easy and shows up only as a wrapped line or a
scrolled frame in a finished GIF, so `record.sh` measures each scene first --
it runs it once with the delays off and not attached to a terminal, where
nothing wraps, and prints the size it actually needs:

```
==> swap  (84x33)
    fits 75x32 of 84x33
```

A scene wider or taller than its terminal gets a warning naming the number to
put in the header.

## Knobs

All environment variables, all with sensible defaults:

| | |
| :-- | :-- |
| `OUT` | where the GIFs go (`docs/`) |
| `WORK` | scratch tree for the binary, fixture and casts (`/tmp/kh3-demo`) |
| `FONT` | font family list; prefers JetBrains Mono, falls back to DejaVu Sans Mono |
| `FONT_SIZE` | pixels (16) |
| `THEME` | agg theme: a name, or `bg,fg,` and eight colors (a warm dark palette built around the `#dfba73` the UI uses) |
| `SPEED` | playback multiplier (1) |

`TYPE_DELAY` and `BEAT` in `lib.sh` set how fast the typing looks and how long
it pauses between commands.

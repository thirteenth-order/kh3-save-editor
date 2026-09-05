# tools

## gen_tables.py

Regenerates `internal/kh3/tables.go` from the enums in `KHSave.Lib3`. Clones
Xeeynamo/KingdomSaveEditor at a pinned commit into `refs/` if it is absent.

```sh
python tools/gen_tables.py           # regenerate
python tools/gen_tables.py --check   # fail if the committed table is stale (CI)
```

The upstream enums carry explicit `= N` anchors that re-base the counter, so a
naive positional parse drifts by several indices. That bug once put Soldier's
Earring at 187 instead of 256. The parser here honors them.

Along with `gen_emblem.py` this is the only Python left in the repository, and
both are build-time codegen rather than anything that ships.

## gen_emblem.py

Draws the web UI's rose-window artwork: `emblem.svg`, `icon.svg`, and the
`rose` and `ring` symbols inside `index.html`.

```sh
python tools/gen_emblem.py           # redraw (make emblem)
python tools/gen_emblem.py --check   # fail if a committed asset is stale (CI)
```

The look is the original's and is meant to stay that way: navy and gold rays
radiating from a gold hub ring, a woven net over the outer band, a gold rim.
What was wrong with it was the setting-out. The backdrop stacked three
symmetries with no common divisor -- eight wedges, sixteen spokes, twelve
circles -- with the circles sized by eye so they overlapped and punched through
the hub while stopping short of the rim; the favicon's comment promised twelve
wedges and drew six; and the emblem was a 117 KB PNG of a fourth drawing that
shared no radius with either.

Now one set of radii, measured off the original with a radial histogram, feeds
all three, and the net's arcs are solved rather than placed. The module
docstring carries the derivations and the calls that were made by rendering and
comparing rather than by reasoning -- the net's `span`, the favicon's ray count.
Change a number there and all three marks move together, which is the point.

## genfixture

Builds synthetic, encrypted saves. Tests and CI use these rather than a real
save, which would embed its owner's SteamID64 and their whole playthrough.

```sh
go run ./tools/genfixture /tmp/fx            # three slots, flat
go run ./tools/genfixture -layout /tmp/fx    # full auto-detectable Steam tree
make fixture                                 # the -layout form, into /tmp/kh3-fixture
```

It is a port of the Python generator that produced `testdata/golden.json`, and
`TestGoldenFixturesStillMatch` fails if it ever drifts from it, since the
vectors are only meaningful against the exact inputs they were built on.

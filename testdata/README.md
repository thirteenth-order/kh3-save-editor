# testdata

## golden.json

90 vectors. Each `sha256` is of the **encrypted** save the tool should write
for that case: an identity round trip, every difficulty against every flag
combination, JSON patches, and a rekey.

They were produced by a second, independent implementation of this save format,
written in Python from the same reverse engineering but not from the Go code.
For a while both ran on every commit and their output was compared byte for
byte on Linux, macOS and Windows. That caught real divergences, including one
where the two disagreed on how an unmapped item name should render.

Once the Go tool reached parity the Python side was removed rather than
maintained twice. It was never committed, so these vectors are the record. Freezing its output keeps what the comparison was actually
worth, which is the evidence that two independently written implementations
agreed on every byte, without the cost of carrying both.

`TestGoldenVectors` replays each case. `TestGoldenFixturesStillMatch` runs
first and checks that `internal/fixture` still produces the exact inputs the
vectors were built on, because if the fixture drifts the vectors mean nothing.

No real save was used. Every input is synthetic, built under the account
`76561190000000000`, which belongs to nobody. A real save embeds its owner's
SteamID64 and their whole playthrough, so none is committed here or anywhere
else in this repository.

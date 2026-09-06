# jscheck

The browser half of this program is about twenty-three hundred lines that no Go
test can reach: a validator, a JSON editor, a form builder that draws itself out
of the published schema, and a dashboard that reads a dump directly. Two checks
cover it, and neither one changes a byte of what ships.

## Running it

`run.js` runs the page's own logic outside a browser, against the same schema
the server serves and a dump of the same fixture the Go tests use, and fails on
anything that throws or answers wrong.

`dom.js` is the smallest DOM those builders actually touch. It is not a browser
and does not try to be one: it exists so that taking a branch runs the code in
it, which is what catches the typo a syntax check cannot.

`internal/gui/js_test.go` writes the two inputs and runs `run.js`.

The dashboard checks assert on values out of the fixture rather than on node
counts, because a panel that renders nothing still renders a panel. They also
assert that the meters are drawn from the schema's soft ranges and that the
provenance notes are the schema's words, which is what stops the page claiming
more confidence than the format work does.

## Type checking

`tsconfig.json` reads the same asset files where they sit, with `checkJs`. It
finds the undefined name, the misspelled property and the wrong argument count
without needing a DOM to run them in, which covers the branches `run.js` never
takes.

```sh
make typecheck          # from the repo root
npm ci && npx tsc -p tsconfig.json   # from here
```

`typescript` is the only dependency, it is pinned exactly, and it has none of
its own. Nothing installed here is served, bundled or shipped: the page is
still classic scripts with no build step, and the binary embeds the assets
exactly as written. `noEmit` is set, so there is no output to leak into one.

## Why both skip without node

The Go suite has no other need of node, and a machine without it should still
be able to run the tests. CI pins node in its own job, so there the two run by
declaration rather than by whatever the runner image happens to ship.

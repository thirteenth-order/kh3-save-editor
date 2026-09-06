# jscheck

The browser half of this program is about twelve hundred lines that no Go test
can reach: a validator, a JSON editor and a form builder that draws itself out
of the published schema. This runs them outside a browser, against the same
schema the server serves and a dump of the same fixture the Go tests use, and
fails on anything that throws or answers wrong.

`dom.js` is the smallest DOM those builders actually touch. It is not a browser
and does not try to be one: it exists so that taking a branch runs the code in
it, which is what catches the typo that a syntax check cannot.

`internal/gui/js_test.go` writes the two inputs and runs `run.js`. It skips when
node is not installed, so the Go suite still passes without it.

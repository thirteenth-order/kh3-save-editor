package gui

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
	"strings"
	"testing"
)

func testServer() *Server {
	s := &Server{token: newToken(), addr: "127.0.0.1:54321", mux: http.NewServeMux()}
	s.mux.HandleFunc("/ok", s.guard(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("reached"))
	}))
	return s
}

func do(s *Server, mutate func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "http://127.0.0.1:54321/ok", nil)
	r.Host = "127.0.0.1:54321"
	r.Header.Set(tokenHeader, s.token)
	if mutate != nil {
		mutate(r)
	}
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	return w
}

func TestValidRequestPasses(t *testing.T) {
	if w := do(testServer(), nil); w.Code != http.StatusOK {
		t.Fatalf("code %d, want 200", w.Code)
	}
}

func TestMissingTokenIsRefused(t *testing.T) {
	s := testServer()
	w := do(s, func(r *http.Request) { r.Header.Del(tokenHeader) })
	if w.Code != http.StatusForbidden {
		t.Fatalf("code %d, want 403", w.Code)
	}
}

func TestWrongTokenIsRefused(t *testing.T) {
	s := testServer()
	w := do(s, func(r *http.Request) { r.Header.Set(tokenHeader, newToken()) })
	if w.Code != http.StatusForbidden {
		t.Fatalf("code %d, want 403", w.Code)
	}
}

// A page on the open web cannot read our token, so this is the attack that
// matters most: a blind cross-origin POST from a malicious tab.
func TestCrossSiteIsRefusedEvenWithAToken(t *testing.T) {
	s := testServer()
	for _, site := range []string{"cross-site", "same-site"} {
		w := do(s, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", site) })
		if w.Code != http.StatusForbidden {
			t.Errorf("Sec-Fetch-Site %q gave %d, want 403", site, w.Code)
		}
	}
}

func TestSameOriginIsAllowed(t *testing.T) {
	s := testServer()
	w := do(s, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") })
	if w.Code != http.StatusOK {
		t.Fatalf("code %d, want 200", w.Code)
	}
}

// DNS rebinding: the attacker's name resolves to 127.0.0.1, but the Host
// header still carries their hostname.
func TestRebindingHostIsRefused(t *testing.T) {
	s := testServer()
	for _, host := range []string{"evil.example.com:54321", "evil.example.com", "127.0.0.1:9999", ""} {
		w := do(s, func(r *http.Request) { r.Host = host })
		if w.Code != http.StatusForbidden {
			t.Errorf("host %q gave %d, want 403", host, w.Code)
		}
	}
}

func TestLoopbackAliasesAreAllowed(t *testing.T) {
	s := testServer()
	for _, host := range []string{"127.0.0.1:54321", "localhost:54321", "[::1]:54321"} {
		w := do(s, func(r *http.Request) { r.Host = host })
		if w.Code != http.StatusOK {
			t.Errorf("host %q gave %d, want 200", host, w.Code)
		}
	}
}

func TestTokenInQueryStringWorks(t *testing.T) {
	s := testServer()
	r := httptest.NewRequest("GET", "http://127.0.0.1:54321/ok?t="+s.token, nil)
	r.Host = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code %d, want 200", w.Code)
	}
}

func TestSecurityHeadersAreSet(t *testing.T) {
	w := do(testServer(), nil)
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP = %q", csp)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("missing no-store")
	}
}

func TestTokensAreDistinctAndLong(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok := newToken()
		if len(tok) != 64 {
			t.Fatalf("token length %d, want 64 hex chars", len(tok))
		}
		if seen[tok] {
			t.Fatal("newToken repeated itself")
		}
		seen[tok] = true
	}
}

// Even with a valid token the browser must not be able to steer writes
// outside a real save folder.
func TestAllowedPathRejectsAnythingOutsideASaveFolder(t *testing.T) {
	tmp := t.TempDir()
	stray := filepath.Join(tmp, "KHIII_slot0.bin")
	os.WriteFile(stray, []byte("x"), 0o644)

	// No folders known: nothing may be written.
	for _, p := range []string{
		stray,                                  // right name, wrong folder
		filepath.Join(tmp, "passwords.txt"),    // wrong name entirely
		filepath.Join(tmp, "KHIII_slot0.bin2"), // wrong suffix
		"/etc/passwd",
		"",
	} {
		if allowedPath(p, nil) {
			t.Errorf("allowedPath(%q) = true, want false", p)
		}
	}
}

// A folder the user pointed us at joins the allowlist, but only for files
// that actually look like saves inside it.
func TestAllowedPathAcceptsAnAddedFolder(t *testing.T) {
	tmp := t.TempDir()
	dirs := []kh3.SaveDir{{Path: tmp}}

	good := filepath.Join(tmp, "KHIII_slot0.bin")
	if !allowedPath(good, dirs) {
		t.Error("a save inside an added folder was refused")
	}
	for _, bad := range []string{
		filepath.Join(tmp, "secrets.txt"),
		filepath.Join(tmp, "nested", "KHIII_slot0.bin"),
		filepath.Join(filepath.Dir(tmp), "KHIII_slot0.bin"),
	} {
		if allowedPath(bad, dirs) {
			t.Errorf("allowedPath(%q) = true, want false", bad)
		}
	}
}

func TestResolveSaveDirFindsTheDataFolder(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "KINGDOM HEARTS III", "Steam", "1", "SaveGames", "kh3sv2", "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(data, "KHIII_slot0.bin"), []byte("x"), 0o644)

	// Pointing anywhere at or above the data folder should work.
	for _, in := range []string{root, filepath.Join(root, "KINGDOM HEARTS III"), data} {
		got, err := resolveSaveDir(in)
		if err != nil {
			t.Errorf("resolveSaveDir(%q): %v", in, err)
			continue
		}
		if got != data {
			t.Errorf("resolveSaveDir(%q) = %q, want %q", in, got, data)
		}
	}

	// A file rather than a folder resolves to its directory.
	if got, err := resolveSaveDir(filepath.Join(data, "KHIII_slot0.bin")); err != nil || got != data {
		t.Errorf("got %q, %v", got, err)
	}

	if _, err := resolveSaveDir(t.TempDir()); err == nil {
		t.Error("an empty folder should be refused")
	}
	if _, err := resolveSaveDir(filepath.Join(root, "nope")); err == nil {
		t.Error("a missing folder should be refused")
	}
}

// The real save is seven levels below Documents, and picking Documents is the
// obvious thing for someone to do. An earlier depth cap of six silently
// refused exactly that.
func TestResolveSaveDirReachesTheRealLayoutDepth(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, "Documents", "KINGDOM HEARTS III", "Steam",
		"76561190000000000", "SaveGames", "kh3sv2", "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(data, "KHIII_slot0.bin"), []byte("x"), 0o644)

	for _, start := range []string{home, filepath.Join(home, "Documents")} {
		got, err := resolveSaveDir(start)
		if err != nil {
			t.Errorf("resolveSaveDir(%q): %v", start, err)
			continue
		}
		if got != data {
			t.Errorf("resolveSaveDir(%q) = %q, want %q", start, got, data)
		}
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &store{path: filepath.Join(dir, "folders.json")}
	s.add("/a")
	s.add("/b")
	s.add("/a") // duplicates are ignored
	if got := s.list(); len(got) != 2 {
		t.Fatalf("list = %v", got)
	}
	if got := loadStore2(s.path).list(); len(got) != 2 {
		t.Errorf("did not persist: %v", got)
	}
	s.remove("/a")
	if got := loadStore2(s.path).list(); len(got) != 1 || got[0] != "/b" {
		t.Errorf("after remove: %v", got)
	}
}

func loadStore2(path string) *store {
	s := &store{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	json.Unmarshal(data, s)
	return s
}

// The UI is plain JS reading these keys by name, so a rename here fails
// silently in the browser. Pin the contract.
func TestJSONContract(t *testing.T) {
	blob, err := json.Marshal(slotInfo{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	json.Unmarshal(blob, &got)
	for _, key := range []string{
		"file", "path", "slot", "difficulty", "difficultyName",
		"level", "playtime", "munny", "location", "account",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("slotInfo is missing the %q key the UI reads", key)
		}
	}

	blob, _ = json.Marshal(scanResult{Dirs: []dirInfo{{}}})
	json.Unmarshal(blob, &got)
	for _, key := range []string{"dirs", "gameRunning"} {
		if _, ok := got[key]; !ok {
			t.Errorf("scanResult is missing the %q key the UI reads", key)
		}
	}

	blob, _ = json.Marshal(dirInfo{})
	json.Unmarshal(blob, &got)
	for _, key := range []string{"path", "platform", "accountId", "cloud", "slots"} {
		if _, ok := got[key]; !ok {
			t.Errorf("dirInfo is missing the %q key the UI reads", key)
		}
	}
}

// Guard against the mismatch that actually happened: the page read slot.name
// while the server sent difficultyName, so the pill rendered blank.
func TestUIReadsOnlyKeysWeSend(t *testing.T) {
	// The markup and the scripts are separate assets now, so scan all of them.
	var page []byte
	for _, name := range []string{"assets/index.html", "assets/diffs.js", "assets/ui.js",
		"assets/editor.js", "assets/schema.js", "assets/forms.js", "assets/overview.js",
		"assets/app.js"} {
		blob, err := assets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		page = append(page, blob...)
	}
	// Populate every field: omitempty would otherwise hide keys the page reads.
	sent := map[string]bool{}
	for _, v := range []any{
		slotInfo{Error: "x"},
		dirInfo{},
		scanResult{},
		swapResponse{Backup: "x", Changes: []string{"x"}, Written: true},
		folderResponse{Path: "x", Removed: "x", Canceled: true},
	} {
		blob, _ := json.Marshal(v)
		var m map[string]any
		json.Unmarshal(blob, &m)
		for k := range m {
			sent[k] = true
		}
	}
	re := regexp.MustCompile(`\b(?:slot|d|data|res|preview)\.([a-zA-Z][a-zA-Z0-9]*)`)
	for _, m := range re.FindAllStringSubmatch(string(page), -1) {
		field := m[1]
		switch field {
		case "checked", "value", "textContent", "className", "length", "message", "join", "map", "some", "innerHTML", "append", "onclick", "onchange", "disabled", "style", "id":
			continue // DOM properties, not payload fields
		}
		if !sent[field] {
			t.Errorf("the page reads %q but no response carries that key", field)
		}
	}
}

// The page loads its stylesheet and script as separate assets, and a <link> or
// <script> tag cannot send the token header. So index.html must go out with the
// token already substituted into those URLs. When it did not, the guard
// answered 403, the browser saw text/plain, and the page rendered unstyled and
// inert with no error anywhere near the cause.
func TestPageAssetsCarryTheToken(t *testing.T) {
	s := &Server{token: newToken(), addr: "127.0.0.1:54321", mux: http.NewServeMux()}
	s.mux.HandleFunc("/", s.guard(s.handleIndex))

	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://127.0.0.1:54321"+path, nil)
		r.Host = "127.0.0.1:54321"
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		return w
	}

	page := get("/?t=" + s.token)
	if page.Code != http.StatusOK {
		t.Fatalf("index: code %d, want 200", page.Code)
	}
	body := page.Body.String()
	if strings.Contains(body, "{{token}}") {
		t.Error("index.html went out with the token placeholder unsubstituted")
	}

	// Every same-origin subresource the page names must be fetchable exactly
	// as written, with no extra headers.
	refs := regexp.MustCompile(`(?:href|src)="(/[^"]+)"`).FindAllStringSubmatch(body, -1)
	if len(refs) < 2 {
		t.Fatalf("expected the page to reference its css and js, found %d refs", len(refs))
	}
	for _, m := range refs {
		w := get(m[1])
		if w.Code != http.StatusOK {
			t.Errorf("%s: code %d, want 200", m[1], w.Code)
		}
		if ct := w.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/plain") {
			t.Errorf("%s: content type %q, which a browser will refuse", m[1], ct)
		}
	}
}

// The page's scripts are ES modules, so the token travels as a path segment
// rather than a query parameter: a relative import inherits the path and not
// the query. That is a third transport for the same token, and a third
// transport is exactly the kind of change that quietly opens a door, so the
// doors it must not open are named here one at a time.
func TestAssetPathTokenIsStillTheOnlyWayIn(t *testing.T) {
	s := &Server{token: newToken(), addr: "127.0.0.1:54321", mux: http.NewServeMux()}
	s.mux.HandleFunc("/", s.guard(s.handleIndex))

	get := func(path string) int {
		r := httptest.NewRequest("GET", "http://127.0.0.1:54321"+path, nil)
		r.Host = "127.0.0.1:54321"
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		return w.Code
	}

	if code := get(assetPrefix + s.token + "/app.js"); code != http.StatusOK {
		t.Errorf("the real asset path gave %d, want 200", code)
	}

	// Everything below must not reach an asset. A wrong token is the obvious
	// one; the rest are the shapes that could hand the guard a token it never
	// should have looked at.
	for _, path := range []string{
		assetPrefix + newToken() + "/app.js", // someone else's token
		assetPrefix + "/app.js",              // no token segment at all
		assetPrefix + s.token + "/",          // token, no file
		assetPrefix + s.token,                // token, nothing after it
		"/app.js",                            // the old query-string form, bare
		"/app.js?t=" + s.token,               // and with a token where one no
		"/ui.js?t=" + s.token,                // longer belongs
	} {
		if code := get(path); code == http.StatusOK {
			t.Errorf("%s: code 200, want a refusal", path)
		}
	}

	// A file that is not on the allow-list is not an asset request, so a valid
	// token in its path is never even read: the answer is the same 403 an
	// unauthenticated caller gets, not a 404 that confirms the token was good.
	if code := get(assetPrefix + s.token + "/../server.go"); code == http.StatusOK {
		t.Error("traversal out of the asset prefix reached something")
	}
	if code := get(assetPrefix + s.token + "/index.html"); code == http.StatusOK {
		t.Error("index.html is servable from the asset prefix")
	}
	if code := get(assetPrefix + s.token + "/nothing.js"); code != http.StatusForbidden {
		t.Errorf("an unlisted name gave %d, want 403 rather than a 404 that "+
			"tells the caller their token was accepted", code)
	}
}

// The page names one entry point and reaches the rest through imports, so the
// allow-list and the import graph have to agree. When they did not, the module
// 404'd, the browser reported it as a bare network error, and the page came up
// blank with nothing in the logs pointing at the missing name.
func TestEveryImportedModuleIsOnTheAllowList(t *testing.T) {
	entries, err := assets.ReadDir("assets")
	if err != nil {
		t.Fatal(err)
	}
	imports := regexp.MustCompile(`(?m)^import\s.*?from\s+"\./([^"]+)"`)
	seen := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		blob, err := assets.ReadFile("assets/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range imports.FindAllStringSubmatch(string(blob), -1) {
			seen++
			if _, ok := staticFiles[m[1]]; !ok {
				t.Errorf("%s imports %q, which staticFiles does not serve", e.Name(), m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("found no imports at all; the scan is not reading the modules")
	}

	// And the other direction, for the entry point the markup names.
	page, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	refs := regexp.MustCompile(`(?:href|src)="/a/{{token}}/([^"]+)"`).
		FindAllStringSubmatch(string(page), -1)
	if len(refs) < 2 {
		t.Fatalf("expected the page to name its stylesheet and entry point, found %d", len(refs))
	}
	for _, m := range refs {
		if _, ok := staticFiles[m[1]]; !ok {
			t.Errorf("index.html asks for %q, which staticFiles does not serve", m[1])
		}
	}
}

// The tightened CSP allows no inline style or script, so neither may creep back
// into the markup: a style attribute or an inline <script> would silently stop
// working in the browser while every Go test still passed.
func TestPageHasNoInlineStyleOrScript(t *testing.T) {
	page, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	body := string(page)
	if strings.Contains(body, "style=\"") {
		t.Error("index.html has a style attribute, which style-src 'self' blocks")
	}
	if regexp.MustCompile(`<script(?:\s[^>]*)?>[^<\s]`).MatchString(body) {
		t.Error("index.html has an inline script, which script-src 'self' blocks")
	}
}

// A save inside a backup zip is written by rewriting the whole archive, so the
// archive is what the allowlist has to gate. Getting this wrong would let a
// page with a valid token name any .zip on the disk and have this process
// rewrite it.
func TestAllowedPathGatesArchivesOnTheArchive(t *testing.T) {
	const member = "KINGDOM HEARTS III/Steam/1/SaveGames/kh3sv2/data/KHIII_slot0.bin"
	listed := filepath.Join(t.TempDir(), "listed.zip")
	dirs := []kh3.SaveDir{{Path: listed, Archive: true}}

	if !allowedPath(kh3.JoinArchive(listed, member), dirs) {
		t.Error("a save inside a listed archive was refused")
	}
	unlisted := filepath.Join(t.TempDir(), "elsewhere.zip")
	for _, bad := range []string{
		// An archive nobody added.
		kh3.JoinArchive(unlisted, member),
		// A listed archive, but not a save inside it.
		kh3.JoinArchive(listed, "KINGDOM HEARTS III/Steam/1/Config/GameSettings.dat"),
		kh3.JoinArchive(listed, "../outside.bin"),
	} {
		if allowedPath(bad, dirs) {
			t.Errorf("allowedPath(%q) = true, want false", bad)
		}
	}
}

// The two kinds of entry must not be able to stand in for each other: an
// archive on the list does not make its own path usable as a directory, and a
// directory on the list does not authorize rewriting a zip of the same name.
func TestArchiveAndFolderEntriesDoNotSubstitute(t *testing.T) {
	p := filepath.Join(t.TempDir(), "saves.zip")

	asArchive := []kh3.SaveDir{{Path: p, Archive: true}}
	if allowedPath(filepath.Join(p, "KHIII_slot0.bin"), asArchive) {
		t.Error("an archive entry authorized a plain file path under it")
	}

	asFolder := []kh3.SaveDir{{Path: p}}
	member := kh3.JoinArchive(p, "KINGDOM HEARTS III/Steam/1/SaveGames/kh3sv2/data/KHIII_slot0.bin")
	if allowedPath(member, asFolder) {
		t.Error("a folder entry authorized rewriting an archive of the same name")
	}
}

func TestResolveSaveDirAcceptsABackupArchive(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "backup.zip")
	writeTestZip(t, good, map[string]string{
		"KINGDOM HEARTS III/Steam/1/SaveGames/kh3sv2/data/KHIII_slot0.bin": "x",
	})
	got, err := resolveSaveDir(good)
	if err != nil {
		t.Fatalf("resolveSaveDir(%q): %v", good, err)
	}
	if got != good {
		t.Errorf("resolved to %q, want the archive itself", got)
	}

	// A zip with nothing of ours in it is a mistake worth reporting.
	empty := filepath.Join(dir, "holiday-photos.zip")
	writeTestZip(t, empty, map[string]string{"beach.jpg": "x"})
	if _, err := resolveSaveDir(empty); err == nil {
		t.Error("expected an error for an archive holding no saves")
	}
}

func writeTestZip(t *testing.T, path string, members map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range members {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// -addr can move the listener off loopback, which is what makes the container
// case work. It must not weaken any other gate: a request published through a
// wildcard bind still needs the token and still has to arrive under a loopback
// Host, so a rebound DNS name is refused exactly as it is on the default bind.
func TestWildcardBindKeepsTheOtherGates(t *testing.T) {
	s := &Server{token: newToken(), addr: "0.0.0.0:8787", mux: http.NewServeMux()}
	s.mux.HandleFunc("/ok", s.guard(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := func(host, token string) int {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8787/ok", nil)
		r.Host = host
		r.Header.Set(tokenHeader, token)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		return w.Code
	}
	if code := req("127.0.0.1:8787", s.token); code != http.StatusOK {
		t.Errorf("published port with a good token: got %d, want 200", code)
	}
	if code := req("127.0.0.1:8787", "wrong"); code != http.StatusForbidden {
		t.Errorf("gate 2 (token) did not hold on a wildcard bind: got %d", code)
	}
	if code := req("evil.example.com:8787", s.token); code != http.StatusForbidden {
		t.Errorf("gate 3 (Host) did not hold on a wildcard bind: got %d", code)
	}
}

// The warning printed at startup keys off this, so a bind that a remote host
// can reach must never be reported as loopback.
func TestIsLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "127.0.0.1:8787", "[::1]:8787", "127.9.9.9:1"} {
		if !isLoopback(addr) {
			t.Errorf("isLoopback(%q) = false, want true", addr)
		}
	}
	for _, addr := range []string{"0.0.0.0:8787", "[::]:8787", "192.168.1.4:8787", "garbage"} {
		if isLoopback(addr) {
			t.Errorf("isLoopback(%q) = true, want false", addr)
		}
	}
}

// 0.0.0.0 is a valid thing to listen on and not a valid thing to open, so the
// URL we print has to name an address the user can actually connect to. Both
// wildcards answer 127.0.0.1: Go reports a 0.0.0.0 bind as [::] on a
// dual-stack host, and `docker run -p` publishes IPv4, so the v6 form would be
// a URL that does not connect.
func TestBrowsableAddr(t *testing.T) {
	for addr, want := range map[string]string{
		"0.0.0.0:8787":   "127.0.0.1:8787",
		"[::]:8787":      "127.0.0.1:8787",
		"127.0.0.1:4111": "127.0.0.1:4111",
		"garbage":        "garbage",
	} {
		if got := browsableAddr(addr); got != want {
			t.Errorf("browsableAddr(%q) = %q, want %q", addr, got, want)
		}
	}
}

// buildSave encrypts a minimal but genuine save for account, so scan() runs the
// real decrypt path rather than a stub.
// buildPlainSave is the save structure with no wrapper, which is what a
// console save tool hands back and what buildSave then encrypts.
func buildPlainSave(difficulty, level byte) []byte {
	const plainLen = 0x20000
	plain := make([]byte, plainLen)
	copy(plain, kh3.Magic)
	binary.LittleEndian.PutUint32(plain[0x04:], plainLen-24)
	binary.LittleEndian.PutUint16(plain[0x08:], 5)
	binary.LittleEndian.PutUint16(plain[0x0A:], 2)
	plain[0x14] = difficulty
	plain[0x2C] = level
	return plain
}

func buildSave(t *testing.T, account string, difficulty byte, level byte) []byte {
	t.Helper()
	plain := buildPlainSave(difficulty, level)

	key, err := kh3.DeriveKey(account)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := kh3.Wrap(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

// The account id is the key material. It used to be masked in place at the top
// of scan() and then handed to ResolveAccount, so every save in the interface
// reported "could not determine the account id" while the folder above it
// displayed the id perfectly. Masking belongs on the way out only.
func TestScanResolvesSavesWhileMaskingTheAccountItReports(t *testing.T) {
	const account = "76561190000000000"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "KHIII_slot0.bin"),
		buildSave(t, account, 3, 7), 0o644); err != nil {
		t.Fatal(err)
	}

	res := scan([]kh3.SaveDir{{Path: dir, Platform: "Steam", AccountID: account}})
	if len(res.Dirs) != 1 || len(res.Dirs[0].Slots) != 1 {
		t.Fatalf("scan returned %d dirs", len(res.Dirs))
	}
	got := res.Dirs[0].Slots[0]
	if got.Error != "" {
		t.Fatalf("slot reported %q; the save is decryptable with the account it was given", got.Error)
	}
	if got.Name != "Critical" || got.Level != 7 {
		t.Errorf("slot decoded as %q level %d, want Critical level 7", got.Name, got.Level)
	}
	// Having resolved it, nothing on the wire may carry the full id.
	if strings.Contains(res.Dirs[0].AccountID, account) || strings.Contains(got.Account, account) {
		t.Error("the response carries the unmasked account id")
	}
	if res.Dirs[0].AccountID == "" || !strings.Contains(res.Dirs[0].AccountID, "*") {
		t.Errorf("folder account %q is not masked", res.Dirs[0].AccountID)
	}
}

// The same, for a save inside a backup archive: the account comes from a
// directory name inside the zip, and is equally key material.
func TestScanResolvesSavesInsideAnArchive(t *testing.T) {
	const account = "76561190000000000"
	const member = "KINGDOM HEARTS III/Steam/" + account + "/SaveGames/kh3sv2/data/KHIII_slot0.bin"
	zipPath := filepath.Join(t.TempDir(), "backup.zip")

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create(member)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(buildSave(t, account, 2, 5)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	res := scan([]kh3.SaveDir{describeDir(zipPath)})
	if len(res.Dirs) != 1 || len(res.Dirs[0].Slots) != 1 {
		t.Fatalf("scan returned %d dirs", len(res.Dirs))
	}
	got := res.Dirs[0].Slots[0]
	if got.Error != "" {
		t.Fatalf("slot inside the archive reported %q", got.Error)
	}
	if got.Name != "Proud" || got.Level != 5 {
		t.Errorf("slot decoded as %q level %d, want Proud level 5", got.Name, got.Level)
	}
	if strings.Contains(res.Dirs[0].AccountID, account) {
		t.Error("the response carries the unmasked account id")
	}
}

// A port mismatch is refused like any other bad Host, but it is the one case
// with a boring cause -- a container published on a different port -- so it
// says so rather than leaving the user to guess.
func TestHostRefusalExplainsAPortMismatch(t *testing.T) {
	msg := hostRefusal("127.0.0.1:9000", "[::]:8787")
	if !strings.Contains(msg, "8787") || !strings.Contains(msg, "9000") {
		t.Errorf("port mismatch message names neither port: %q", msg)
	}
	// Everything else stays terse: a rebound hostname learns nothing here.
	for _, host := range []string{"evil.example.com:9000", "evil.example.com", "garbage"} {
		if got := hostRefusal(host, "[::]:8787"); got != "bad host" {
			t.Errorf("hostRefusal(%q) = %q, want %q", host, got, "bad host")
		}
	}
}

// --- detail and patch ------------------------------------------------------

// saveTree writes one save under the directory layout Steam actually uses, so
// the account id is discoverable the same way it is in production: as a
// directory name on the way to the file.
func saveTree(t *testing.T, account string, difficulty, level byte) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Steam", account, "SaveGames", "kh3sv2", "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "KHIII_slot0.bin")
	if err := os.WriteFile(p, buildSave(t, account, difficulty, level), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, p
}

// accountHint is what lets a folder the user added by hand work even when the
// id is not on the path: the scan already worked it out for that folder.
func TestAccountHintPrefersTheMostSpecificFolder(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "saves")
	inner := filepath.Join(root, "player", "data")
	dirs := []kh3.SaveDir{
		{Path: root, AccountID: "76561190000000000"},
		{Path: inner, AccountID: "76561190000000001"},
		{Path: filepath.Join(string(filepath.Separator), "elsewhere"), AccountID: "76561190000000002"},
	}
	if got := accountHint(filepath.Join(inner, "KHIII_slot0.bin"), dirs); got != "76561190000000001" {
		t.Errorf("hint = %q, want the id of the innermost folder holding the save", got)
	}
	if got := accountHint(filepath.Join(root, "KHIII_slot0.bin"), dirs); got != "76561190000000000" {
		t.Errorf("hint = %q, want the id of the folder that holds the save", got)
	}
	if got := accountHint(filepath.Join(string(filepath.Separator), "nowhere", "KHIII_slot0.bin"), dirs); got != "" {
		t.Errorf("hint = %q for a path in no known folder, want empty", got)
	}
}

// apiServer wires the real routes, so these tests exercise the same guard the
// browser hits rather than calling the handlers directly.
func apiServer(dirs []kh3.SaveDir) *Server {
	s := &Server{token: newToken(), addr: "127.0.0.1:54321", mux: http.NewServeMux(),
		folder: &store{path: filepath.Join(os.TempDir(), "kh3-nonexistent-store.json")}}
	for _, d := range dirs {
		s.folder.add(d.Path)
	}
	s.mux.HandleFunc("/api/detail", s.guard(s.handleDetail))
	s.mux.HandleFunc("/api/patch", s.guard(s.handlePatch))
	return s
}

func call(s *Server, method, url string, body []byte) *httptest.ResponseRecorder {
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, url, nil)
	} else {
		r = httptest.NewRequest(method, url, bytes.NewReader(body))
	}
	r.Host = "127.0.0.1:54321"
	r.Header.Set(tokenHeader, s.token)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	return w
}

func TestDetailRendersTheSameDocumentAsDump(t *testing.T) {
	const account = "76561190000000000"
	dir, p := saveTree(t, account, 3, 7)
	s := apiServer([]kh3.SaveDir{{Path: dir, Platform: "added", AccountID: account}})

	w := call(s, "GET", "http://127.0.0.1:54321/api/detail?path="+url.QueryEscape(p), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("code %d: %s", w.Code, w.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"header", "characters", "inventory", "party", "magic",
		"links", "shortcuts", "story_flags", "materials", "records"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("the document has no %q section", key)
		}
	}
	// The account id is the key material and a dump is what people paste into
	// a bug report, so it must not be in there at all.
	if doc["account"] != nil {
		t.Errorf("detail carries an account id: %v", doc["account"])
	}
	if strings.Contains(w.Body.String(), account) {
		t.Error("the detail response carries the account id somewhere")
	}
}

func TestDetailRefusesAPathOutsideASaveFolder(t *testing.T) {
	dir, _ := saveTree(t, "76561190000000000", 1, 6)
	s := apiServer([]kh3.SaveDir{{Path: dir, Platform: "added"}})
	outside := filepath.Join(t.TempDir(), "KHIII_slot0.bin")
	w := call(s, "GET", "http://127.0.0.1:54321/api/detail?path="+url.QueryEscape(outside), nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("code %d, want 403", w.Code)
	}
}

func TestPatchAppliesAndBacksUp(t *testing.T) {
	const account = "76561190000000000"
	dir, p := saveTree(t, account, 1, 6)
	s := apiServer([]kh3.SaveDir{{Path: dir, Platform: "added", AccountID: account}})

	body := []byte(`{"path":` + quote(p) + `,"doc":{"header":{"munny":4242}}}`)
	w := call(s, "POST", "http://127.0.0.1:54321/api/patch", body)
	if w.Code != http.StatusOK {
		t.Fatalf("code %d: %s", w.Code, w.Body.String())
	}
	var res swapResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Written || res.Backup == "" {
		t.Fatalf("written=%v backup=%q; a write must always leave a backup", res.Written, res.Backup)
	}
	if _, err := os.Stat(res.Backup); err != nil {
		t.Errorf("the backup it reported does not exist: %v", err)
	}
	// The file on disk must now decrypt and read back the new value.
	blob, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	key, err := kh3.DeriveKey(account)
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := kh3.Open(blob, key)
	if err != nil {
		t.Fatalf("the save it wrote does not reopen: %v", err)
	}
	if got := kh3.ReadHeader(plain).Munny; got != 4242 {
		t.Errorf("munny = %d, want 4242", got)
	}
}

func TestPatchDryRunWritesNothing(t *testing.T) {
	const account = "76561190000000000"
	dir, p := saveTree(t, account, 1, 6)
	s := apiServer([]kh3.SaveDir{{Path: dir, Platform: "added", AccountID: account}})
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"path":` + quote(p) + `,"dryRun":true,"doc":{"header":{"munny":4242}}}`)
	w := call(s, "POST", "http://127.0.0.1:54321/api/patch", body)
	if w.Code != http.StatusOK {
		t.Fatalf("code %d: %s", w.Code, w.Body.String())
	}
	var res swapResponse
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Written || len(res.Changes) == 0 {
		t.Errorf("written=%v changes=%v; a dry run reports and writes nothing", res.Written, res.Changes)
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a dry run changed the file")
	}
}

func TestPatchRefusesAPathOutsideASaveFolder(t *testing.T) {
	dir, _ := saveTree(t, "76561190000000000", 1, 6)
	s := apiServer([]kh3.SaveDir{{Path: dir, Platform: "added"}})
	outside := filepath.Join(t.TempDir(), "KHIII_slot0.bin")
	body := []byte(`{"path":` + quote(outside) + `,"doc":{"header":{"munny":1}}}`)
	if w := call(s, "POST", "http://127.0.0.1:54321/api/patch", body); w.Code != http.StatusForbidden {
		t.Fatalf("code %d, want 403", w.Code)
	}
}

func TestPatchRejectsABadDocument(t *testing.T) {
	const account = "76561190000000000"
	dir, p := saveTree(t, account, 1, 6)
	s := apiServer([]kh3.SaveDir{{Path: dir, Platform: "added", AccountID: account}})
	for _, doc := range []string{`{"materials":{"999":1}}`, `{"characters":{"Nobody":{"hp":1}}}`} {
		body := []byte(`{"path":` + quote(p) + `,"doc":` + doc + `}`)
		if w := call(s, "POST", "http://127.0.0.1:54321/api/patch", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s gave %d, want 400", doc, w.Code)
		}
	}
}

func TestPatchIsPostOnly(t *testing.T) {
	dir, _ := saveTree(t, "76561190000000000", 1, 6)
	s := apiServer([]kh3.SaveDir{{Path: dir, Platform: "added"}})
	if w := call(s, "GET", "http://127.0.0.1:54321/api/patch", nil); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code %d, want 405", w.Code)
	}
}

// A save with no Steam wrapper must be listed and editable, not reported as
// "could not determine the account id" for a file we can read perfectly well.
func TestScanAndPatchHandleASaveWithNoWrapper(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "KHIII_slot0.bin")
	plain, err := kh3.Seal(buildPlainSave(3, 7), kh3.FormatPlain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, plain, 0o644); err != nil {
		t.Fatal(err)
	}

	res := scan([]kh3.SaveDir{{Path: dir, Platform: "added"}})
	got := res.Dirs[0].Slots[0]
	if got.Error != "" {
		t.Fatalf("an unwrapped save reported %q", got.Error)
	}
	if got.Format != "plain" || got.Account != "" {
		t.Errorf("format %q account %q, want plain and no account", got.Format, got.Account)
	}
	if got.Name != "Critical" || got.Level != 7 {
		t.Errorf("decoded as %q level %d", got.Name, got.Level)
	}

	s := apiServer([]kh3.SaveDir{{Path: dir, Platform: "added"}})
	body := []byte(`{"path":` + quote(p) + `,"doc":{"header":{"munny":99}}}`)
	w := call(s, "POST", "http://127.0.0.1:54321/api/patch", body)
	if w.Code != http.StatusOK {
		t.Fatalf("patching an unwrapped save gave %d: %s", w.Code, w.Body.String())
	}
	blob, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if kh3.DetectFormat(blob) != kh3.FormatPlain {
		t.Error("the write changed the container form")
	}
	back, _, err := kh3.Open(blob, nil)
	if err != nil {
		t.Fatalf("the save it wrote does not reopen: %v", err)
	}
	if got := kh3.ReadHeader(back).Munny; got != 99 {
		t.Errorf("munny = %d, want 99", got)
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// The page builds its whole editor out of /api/schema, so a field renamed in
// Go and not in the script -- or the other way round -- is a panel that
// silently renders nothing. Same idea as TestUIReadsOnlyKeysWeSend, for the
// other payload.
func TestUIReadsOnlySchemaKeysWeSend(t *testing.T) {
	var page []byte
	for _, name := range []string{"assets/schema.js", "assets/forms.js",
		"assets/overview.js", "assets/app.js"} {
		blob, err := assets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		page = append(page, blob...)
	}

	// Marshal a schema and collect every key name that appears anywhere in it,
	// at any depth: the script reaches into sections, fields and entries alike.
	blob, err := json.Marshal(kh3.Describe())
	if err != nil {
		t.Fatal(err)
	}
	var tree any
	if err := json.Unmarshal(blob, &tree); err != nil {
		t.Fatal(err)
	}
	sent := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, sub := range t {
				sent[k] = true
				walk(sub)
			}
		case []any:
			for _, sub := range t {
				walk(sub)
			}
		}
	}
	walk(tree)
	// Names the script gives a schema value locally, and the DOM properties it
	// reads off the same variables.
	for _, ours := range []string{"length", "indexOf", "filter", "some", "map", "slice",
		"split", "push", "list", "byId", "has", "get", "node", "input", "checked", "value"} {
		sent[ours] = true
	}

	re := regexp.MustCompile(`\b(?:SCHEMA|sec|sub|f)\.([a-zA-Z][a-zA-Z0-9_]*)`)
	for _, m := range re.FindAllStringSubmatch(string(page), -1) {
		if !sent[m[1]] {
			t.Errorf("the page reads schema key %q, which /api/schema does not carry", m[1])
		}
	}
}

// The schema is what the editor is built out of, so the endpoint has to answer
// with something the page can use rather than merely with 200.
func TestSchemaEndpointDescribesTheDocument(t *testing.T) {
	s := &Server{token: newToken(), addr: "127.0.0.1:54321", mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/schema", s.guard(s.handleSchema))

	req := httptest.NewRequest("GET", "http://127.0.0.1:54321/api/schema", nil)
	req.Host = "127.0.0.1:54321"
	req.Header.Set(tokenHeader, s.token)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code %d, want 200", w.Code)
	}
	var got kh3.Schema
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.DocFormat != kh3.DocFormat {
		t.Errorf("docFormat %q, want %q", got.DocFormat, kh3.DocFormat)
	}
	if len(got.Sections) != len(kh3.Sections()) {
		t.Errorf("%d sections, want %d", len(got.Sections), len(kh3.Sections()))
	}
	if len(got.Tables) == 0 || len(got.EquipTables) == 0 {
		t.Error("the schema went out with no tables, so every picker would be empty")
	}
	// Cached after the first call, and the cache must not hand back something
	// different.
	w2 := httptest.NewRecorder()
	s.mux.ServeHTTP(w2, req)
	if w2.Body.String() != w.Body.String() {
		t.Error("the second call answered differently from the first")
	}
}

func TestSchemaEndpointNeedsTheToken(t *testing.T) {
	s := &Server{token: newToken(), addr: "127.0.0.1:54321", mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/schema", s.guard(s.handleSchema))
	req := httptest.NewRequest("GET", "http://127.0.0.1:54321/api/schema", nil)
	req.Host = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("code %d, want 403", w.Code)
	}
}

// openSave hands the hint to ResolveAccount as an assertion, so describeDir
// must only produce one when the directory in that position really is an
// account id. An Epic save unpacked with no account level puts the platform
// name there, and asserting that as the account fails a file the
// auto-detection reads without any help at all.
func TestAddedFolderOnlyClaimsAnAccountThatLooksLikeOne(t *testing.T) {
	cases := []struct {
		dir     string
		account string
	}{
		{filepath.Join("KINGDOM HEARTS III", "Steam", "76561190000000000",
			"SaveGames", "kh3sv2", "data"), "76561190000000000"},
		{filepath.Join("KINGDOM HEARTS III", "Epic Games Store", kh3.EpicAccount,
			"SaveGames", "kh3sv2", "data"), kh3.EpicAccount},
		{filepath.Join("KINGDOM HEARTS III", "Epic Games Store",
			"SaveGames", "kh3sv2", "data"), ""},
		{filepath.Join("unpacked", "data"), ""},
	}
	for _, c := range cases {
		p := filepath.Join(t.TempDir(), c.dir)
		if got := describeDir(p).AccountID; got != c.account {
			t.Errorf("describeDir(%q) account = %q, want %q", c.dir, got, c.account)
		}
	}
}

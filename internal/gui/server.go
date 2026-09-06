// Package gui serves a small local web UI for editing KH3 saves.
//
// Security model. This process reads and writes files anywhere the user can,
// and it exposes that over HTTP. A page the user happens to have open in
// another tab can issue cross-origin requests to 127.0.0.1. The browser will
// block it from *reading* the response, but the side effect would still land.
// DNS rebinding can defeat even that. So every request must clear four gates:
//
//  1. the listener is bound to 127.0.0.1, never 0.0.0.0
//  2. a random per-run token, handed over only in the launch URL
//  3. the Host header must be the loopback address we are listening on,
//     which is what stops DNS rebinding
//  4. Sec-Fetch-Site, when the browser sends it, must not be cross-site
//
// Gate 1 is the only one that can be relaxed, with -addr, and it exists for
// exactly one case: inside a container, where loopback is not reachable from
// the host and the container boundary is what the port is published behind.
// Gates 2 to 4 hold in every configuration, so a published port still refuses
// a request that arrives without the token or under someone else's hostname.
package gui

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

//go:embed assets
var assets embed.FS

const tokenHeader = "X-KH3-Token"

type Server struct {
	token  string
	addr   string
	mux    *http.ServeMux
	folder *store

	schemaOnce sync.Once
	schemaJSON []byte
	schemaErr  error
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("no entropy available: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// guard applies the four gates described above.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Gate 3: Host must be the loopback address we listen on. A rebound
		// DNS name arrives with the attacker's hostname here.
		if !hostAllowed(r.Host, s.addr) {
			http.Error(w, hostRefusal(r.Host, s.addr), http.StatusForbidden)
			return
		}
		// Gate 4: reject genuine cross-site requests. Browsers that do not
		// send the header fall through to the token check.
		switch r.Header.Get("Sec-Fetch-Site") {
		case "cross-site", "same-site":
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		// Gate 2: the token, from a header or the query string.
		got := r.Header.Get(tokenHeader)
		if got == "" {
			got = r.URL.Query().Get("t")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			http.Error(w, "bad or missing token", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// The page's CSS and JS are served from here as files, so neither
		// inline styles nor inline script need to be allowed at all, and
		// nothing off this origin may load.
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; "+
				"connect-src 'self'; form-action 'none'; frame-ancestors 'none'; base-uri 'none'")
		next(w, r)
	}
}

func hostAllowed(host, addr string) bool {
	if host == addr {
		return true
	}
	h, p, err := net.SplitHostPort(host)
	if err != nil {
		return false
	}
	_, want, err := net.SplitHostPort(addr)
	if err != nil || p != want {
		return false
	}
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

// hostRefusal explains a rejected Host when the reason is mundane. The gate
// compares the port too, so publishing a container on a host port other than
// the one it listens on is refused -- correctly, and for a reason nobody would
// guess from "bad host".
func hostRefusal(host, addr string) string {
	h, p, err := net.SplitHostPort(host)
	if err != nil {
		return "bad host"
	}
	_, want, wantErr := net.SplitHostPort(addr)
	if wantErr != nil || p == want {
		return "bad host"
	}
	if h != "127.0.0.1" && h != "localhost" && h != "::1" {
		return "bad host"
	}
	return fmt.Sprintf("bad host: this UI is serving port %s, but you reached it on "+
		"port %s. If it is in a container, publish it on the same port it listens "+
		"on (-p 127.0.0.1:%s:%s), or set -addr to the published port.", want, p, want, want)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, format string, a ...any) {
	writeJSON(w, code, map[string]string{"error": fmt.Sprintf(format, a...)})
}

// --- payloads ---------------------------------------------------------------

type slotInfo struct {
	File string `json:"file"`
	// Path is what the API acts on. DisplayPath is what the interface renders:
	// on Steam the save lives under a directory named by the account id, so
	// showing the raw path publishes the id the chip beside it is masking.
	Path        string `json:"path"`
	DisplayPath string `json:"displayPath"`
	Slot        string `json:"slot"`
	Difficulty  int    `json:"difficulty"`
	Name        string `json:"difficultyName"`
	Level       int    `json:"level"`
	Playtime    string `json:"playtime"`
	Munny       int    `json:"munny"`
	Location    string `json:"location"`
	Account     string `json:"account"`
	// Format is "steam" or "plain". A plain save has no account id, so the
	// interface shows the form instead of an empty account chip.
	Format string `json:"format"`
	World  string `json:"world"`
	// Whether the Critical start items would do anything for this save, so the
	// interface can show those switches only when they would.
	CanGrant  bool   `json:"canGrantStartItem"`
	CanRevoke bool   `json:"canRevokeStartItem"`
	Error     string `json:"error,omitempty"`
}

type scanResult struct {
	Dirs        []dirInfo `json:"dirs"`
	GameRunning bool      `json:"gameRunning"`
	CanBrowse   bool      `json:"canBrowse"`
}

type dirInfo struct {
	kh3.SaveDir
	DisplayPath string     `json:"displayPath"`
	Slots       []slotInfo `json:"slots"`
}

func scan(dirs []kh3.SaveDir) scanResult {
	out := scanResult{GameRunning: kh3.GameIsRunning()}
	for _, d := range dirs {
		di := dirInfo{SaveDir: d, DisplayPath: kh3.MaskPath(d.Path)}
		// Mask on the way out, and only there. The account id is the key
		// material: masking it in place and then handing it to ResolveAccount
		// derives a key from "765611*******0000", which decrypts nothing, and
		// every save in the UI reported "could not determine the account id".
		di.AccountID = kh3.MaskAccount(d.AccountID)
		for _, p := range kh3.ListSaves(d.Path) {
			base := kh3.Base(p)
			si := slotInfo{File: base, Path: p, DisplayPath: kh3.MaskPath(p), Slot: strings.TrimSuffix(strings.TrimPrefix(base, "KHIII_"), ".bin")}
			blob, err := kh3.ReadFile(p)
			if err != nil {
				si.Error = err.Error()
				di.Slots = append(di.Slots, si)
				continue
			}
			// A save with no Steam wrapper carries no account id and needs no
			// key. Asking ResolveAccount about one would fail and the slot
			// would show "could not determine the account id" for a file this
			// tool can read perfectly well.
			var key []byte
			format := kh3.DetectFormat(blob)
			if format.NeedsKey() {
				acct, k, err := kh3.ResolveAccount(p, blob, d.AccountID)
				if err != nil {
					si.Error = "could not determine the account id"
					di.Slots = append(di.Slots, si)
					continue
				}
				key = k
				si.Account = kh3.MaskAccount(acct)
			}
			plain, _, err := kh3.Open(blob, key)
			if err != nil {
				si.Error = err.Error()
				di.Slots = append(di.Slots, si)
				continue
			}
			si.Format = format.String()
			h := kh3.ReadHeader(plain)
			si.Difficulty = int(h.Difficulty)
			si.Name = kh3.Difficulties[h.Difficulty]
			if !kh3.IsSlot(plain) {
				si.Slot = "system"
				si.Name = ""
				di.Slots = append(di.Slots, si)
				continue
			}
			si.Level = int(h.Level)
			si.Playtime = h.Playtime()
			si.Munny = int(h.Munny)
			si.Location = h.MapPath
			si.World = kh3.WorldName(int(h.WorldLogo))
			si.CanGrant, si.CanRevoke = kh3.StartItemState(plain)
			di.Slots = append(di.Slots, si)
		}
		out.Dirs = append(out.Dirs, di)
	}
	return out
}

type swapRequest struct {
	Path       string `json:"path"`
	Difficulty int    `json:"difficulty"`
	GrantItems bool   `json:"grantStartItems"`
	RevokeItem bool   `json:"revokeStartItems"`
	NoScaleHP  bool   `json:"noScaleHP"`
	DryRun     bool   `json:"dryRun"`
}

// folderResponse is the reply from /api/browse and /api/folder.
type folderResponse struct {
	Path     string `json:"path,omitempty"`
	Removed  string `json:"removed,omitempty"`
	Canceled bool   `json:"canceled,omitempty"`
}

// patchRequest carries a document straight through to kh3.Patch. Doc is raw
// JSON rather than a decoded map so the patch layer sees exactly the bytes the
// caller sent, the same as the CLI reading a file.
type patchRequest struct {
	Path   string          `json:"path"`
	Doc    json.RawMessage `json:"doc"`
	DryRun bool            `json:"dryRun"`
}

type swapResponse struct {
	Changes []string `json:"changes"`
	Backup  string   `json:"backup,omitempty"`
	Written bool     `json:"written"`
}

// allowedPath refuses to touch anything outside a discovered save directory.
// The browser must not be able to talk this process into writing arbitrary
// files, even with a valid token.
func allowedPath(p string, dirs []kh3.SaveDir) bool {
	// A save inside a backup zip. Writing it rewrites the whole archive, so
	// the archive itself is what has to be on the list, and the member has to
	// name a save rather than some other file the archive happens to carry.
	if archive, member, ok := kh3.SplitArchive(p); ok {
		base := path.Base(member)
		if !strings.HasPrefix(base, "KHIII_") || !strings.HasSuffix(base, ".bin") {
			return false
		}
		abs, err := filepath.Abs(archive)
		if err != nil {
			return false
		}
		for _, d := range dirs {
			if d.Archive && d.Path == abs {
				return true
			}
		}
		return false
	}

	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	base := filepath.Base(abs)
	if !strings.HasPrefix(base, "KHIII_") || !strings.HasSuffix(base, ".bin") {
		return false
	}
	dir := filepath.Dir(abs)
	for _, d := range dirs {
		if d.Path == dir && !d.Archive {
			return true
		}
	}
	return false
}

// saveDirs is everything we auto-detect plus every folder the user has
// pointed us at. Both together form the write allowlist.
func (s *Server) saveDirs() []kh3.SaveDir {
	dirs := kh3.FindSaveDirs()
	seen := map[string]bool{}
	for _, d := range dirs {
		seen[d.Path] = true
	}
	for _, p := range s.folder.list() {
		if seen[p] {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		seen[p] = true
		dirs = append(dirs, describeDir(p))
	}
	return dirs
}

// describeDir works out platform/account/cloud for a folder the user chose,
// which may sit anywhere.
func describeDir(p string) kh3.SaveDir {
	if kh3.IsArchive(p) {
		return kh3.DescribeArchive(p)
	}
	d := kh3.SaveDir{Path: p, Platform: "added", AccountID: ""}
	acct := filepath.Dir(filepath.Dir(filepath.Dir(p)))
	// Only take it as an account id if it looks like one. openSave passes this
	// to ResolveAccount as an assertion, not a guess, so a directory that is
	// merely in that position, such as "Epic Games Store" in the layout with
	// no account level, would hard-fail a file the auto-detection reads fine.
	if base := filepath.Base(acct); kh3.IsAccountID(base) {
		d.AccountID = base
		if plat := filepath.Base(filepath.Dir(acct)); plat != "." {
			d.Platform = plat
		}
	}
	if _, err := os.Stat(filepath.Join(acct, "steam_autocloud.vdf")); err == nil {
		d.Cloud = true
	}
	return d
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	res := scan(s.saveDirs())
	res.CanBrowse = pickerAvailable()
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	picked, err := pickFolder()
	if err == ErrNoPicker {
		fail(w, http.StatusNotImplemented,
			"no folder dialog on this system; paste the path instead")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if picked == "" { // the user canceled
		writeJSON(w, http.StatusOK, folderResponse{Canceled: true})
		return
	}
	s.addFolder(w, picked)
}

func (s *Server) handleAddFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req struct {
		Path   string `json:"path"`
		Remove bool   `json:"remove"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "bad request: %v", err)
		return
	}
	if req.Remove {
		s.folder.remove(req.Path)
		writeJSON(w, http.StatusOK, folderResponse{Removed: req.Path})
		return
	}
	s.addFolder(w, req.Path)
}

func (s *Server) addFolder(w http.ResponseWriter, raw string) {
	dir, err := resolveSaveDir(raw)
	if err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	s.folder.add(dir)
	writeJSON(w, http.StatusOK, folderResponse{Path: dir})
}

// openSave reads a save in whichever form it is stored in. A save with no
// Steam wrapper needs no account id, so none of the account plumbing runs for
// one and the endpoints below work on it exactly as they do on a PC save.
type openedSave struct {
	plain  []byte
	key    []byte
	format kh3.Format
}

// accountHint returns the account id of the discovered folder that holds p.
// Usually the id is also a directory name on the way to the save and would be
// found anyway, but a folder the user added by hand need not be laid out that
// way, and the scan already knows the answer for it.
func accountHint(p string, dirs []kh3.SaveDir) string {
	container := kh3.Container(p)
	best := ""
	for _, d := range dirs {
		if d.AccountID == "" {
			continue
		}
		if d.Path == container || strings.HasPrefix(container, d.Path+string(filepath.Separator)) {
			// Prefer the most specific folder, so nested roots do not hand
			// back the id of an unrelated account higher up the tree.
			if len(d.Path) > len(best) {
				best = d.Path
			}
		}
	}
	for _, d := range dirs {
		if d.Path == best {
			return d.AccountID
		}
	}
	return ""
}

func openSave(p string, account string) (*openedSave, error) {
	blob, err := kh3.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var key []byte
	format := kh3.DetectFormat(blob)
	if format.NeedsKey() {
		if _, key, err = kh3.ResolveAccount(p, blob, account); err != nil {
			return nil, err
		}
	}
	plain, _, err := kh3.Open(blob, key)
	if err != nil {
		return nil, err
	}
	return &openedSave{plain: plain, key: key, format: format}, nil
}

// writeSave seals, re-reads its own output the way the game would, backs up
// the file it is about to replace and only then writes. Every write path in
// this program does exactly this, and none of them may skip a step.
func writeSave(p string, o *openedSave, newPlain []byte) (string, error) {
	outBlob, err := kh3.Seal(newPlain, o.format, o.key)
	if err != nil {
		return "", err
	}
	check, _, err := kh3.Open(outBlob, o.key)
	if err != nil {
		return "", fmt.Errorf("self-check failed, nothing written: %w", err)
	}
	// Seal rewrites the CRC at 0x0C, so compare around it.
	if string(check[:0x0C]) != string(newPlain[:0x0C]) ||
		string(check[0x10:]) != string(newPlain[0x10:]) {
		return "", fmt.Errorf("self-check failed, nothing written")
	}
	// For a save inside a zip this copies the whole archive, which is what a
	// member replacement actually rewrites.
	bak, err := kh3.BackupOf(p, time.Now())
	if err != nil {
		return "", fmt.Errorf("could not write a backup, so nothing was changed: %w", err)
	}
	if err := kh3.WriteFile(p, outBlob, 0o644); err != nil {
		return "", err
	}
	return bak, nil
}

func (s *Server) handleSwap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req swapRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "bad request: %v", err)
		return
	}
	dirs := s.saveDirs()
	if !allowedPath(req.Path, dirs) {
		fail(w, http.StatusForbidden, "refusing to touch a file outside a detected save folder")
		return
	}
	if _, ok := kh3.Difficulties[byte(req.Difficulty)]; !ok {
		fail(w, http.StatusBadRequest, "difficulty must be 0-3")
		return
	}

	o, err := openSave(req.Path, accountHint(req.Path, dirs))
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if !kh3.IsSlot(o.plain) {
		fail(w, http.StatusBadRequest, "that is the system file, it has no difficulty")
		return
	}

	newPlain, changes, err := kh3.SwapDifficulty(o.plain, byte(req.Difficulty), kh3.SwapOptions{
		ScaleHP:          !req.NoScaleHP,
		GrantStartItems:  req.GrantItems,
		RevokeStartItems: req.RevokeItem,
	})
	if err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	resp := swapResponse{Changes: changes}
	if req.DryRun || len(changes) == 0 {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	bak, err := writeSave(req.Path, o, newPlain)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	resp.Backup, resp.Written = bak, true
	writeJSON(w, http.StatusOK, resp)
}

// handleDetail renders one save as the same JSON document the dump subcommand
// writes, so the interface and the CLI describe a save identically.
//
// The account id is deliberately not included. It is the key material, and a
// dump is the thing people paste into a bug report.
func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	dirs := s.saveDirs()
	if !allowedPath(p, dirs) {
		fail(w, http.StatusForbidden, "refusing to read a file outside a detected save folder")
		return
	}
	o, err := openSave(p, accountHint(p, dirs))
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	doc, err := kh3.Dump(o.plain, "", kh3.CharCount)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(doc)
}

// handleSchema hands the interface the description of every editable field:
// what each one is called in a dump, what kind of value it holds, which table
// names its ids and what range the format allows. The page builds its editor
// out of this rather than carrying a hand-written widget per field, which is
// what keeps the two from drifting -- and kh3.Describe is the same description
// the format tests hold against a dump.
//
// It is static for the life of the process, so it is worth caching: the enum
// tables come to a few hundred kilobytes and the page asks for them once per
// save it opens.
func (s *Server) handleSchema(w http.ResponseWriter, r *http.Request) {
	s.schemaOnce.Do(func() {
		s.schemaJSON, s.schemaErr = json.Marshal(kh3.Describe())
	})
	if s.schemaErr != nil {
		fail(w, http.StatusInternalServerError, "%v", s.schemaErr)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(s.schemaJSON)
}

// handlePatch applies a JSON document to a save, through exactly the same
// validation the patch subcommand uses. This is what makes every field the
// format layer knows about editable from the interface without the interface
// having to grow a widget for each one.
func (s *Server) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req patchRequest
	// A whole dump comes back a good deal larger than a swap request: the
	// ability arrays alone are 512 entries per character.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "bad request: %v", err)
		return
	}
	dirs := s.saveDirs()
	if !allowedPath(req.Path, dirs) {
		fail(w, http.StatusForbidden, "refusing to touch a file outside a detected save folder")
		return
	}
	if len(req.Doc) == 0 {
		fail(w, http.StatusBadRequest, "no document")
		return
	}
	o, err := openSave(req.Path, accountHint(req.Path, dirs))
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	newPlain, changes, err := kh3.Patch(o.plain, req.Doc)
	if err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	resp := swapResponse{Changes: changes}
	if req.DryRun || len(changes) == 0 {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	bak, err := writeSave(req.Path, o, newPlain)
	if err != nil {
		fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	resp.Backup, resp.Written = bak, true
	writeJSON(w, http.StatusOK, resp)
}

// staticFiles are the only assets served, by exact name. The emblem and the
// sigils inlined in index.html are original artwork, generated by
// tools/gen_emblem.py; no Square Enix or Disney material is bundled.
var staticFiles = map[string]string{
	"/":            "index.html",
	"/app.css":     "app.css",
	"/ui.js":       "ui.js",
	"/editor.js":   "editor.js",
	"/schema.js":   "schema.js",
	"/forms.js":    "forms.js",
	"/overview.js": "overview.js",
	"/app.js":      "app.js",
	"/icon.svg":    "icon.svg",
	"/emblem.svg":  "emblem.svg",
}

var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".svg":  "image/svg+xml",
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	name, ok := staticFiles[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	sub, _ := fs.Sub(assets, "assets")
	data, err := fs.ReadFile(sub, name)
	if err != nil {
		http.Error(w, "missing asset", http.StatusInternalServerError)
		return
	}
	// A <link> or <script> tag cannot set the token header, so the page's own
	// subresource URLs carry the token in the query string instead. Same gate,
	// same value, just the transport a tag supports. Without this the guard
	// answers 403 and the browser drops the stylesheet on its MIME type.
	if name == "index.html" {
		data = bytes.ReplaceAll(data, []byte("{{token}}"), []byte(s.token))
	}
	w.Header().Set("Content-Type", contentTypes[filepath.Ext(name)])
	w.Write(data)
}

// Serve starts the UI and, unless noBrowser, opens it.
// DefaultAddr is gate 1: loopback only, and a port the OS picks fresh each run.
const DefaultAddr = "127.0.0.1:0"

// Serve starts the UI on addr. An empty addr means DefaultAddr; anything else
// is an explicit opt-out of gate 1 and is announced as such.
func Serve(noBrowser bool, addr string) error {
	if addr == "" {
		addr = DefaultAddr
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s := &Server{
		token:  newToken(),
		addr:   ln.Addr().String(),
		mux:    http.NewServeMux(),
		folder: loadStore(),
	}
	s.mux.HandleFunc("/", s.guard(s.handleIndex))
	s.mux.HandleFunc("/api/scan", s.guard(s.handleScan))
	s.mux.HandleFunc("/api/swap", s.guard(s.handleSwap))
	s.mux.HandleFunc("/api/schema", s.guard(s.handleSchema))
	s.mux.HandleFunc("/api/detail", s.guard(s.handleDetail))
	s.mux.HandleFunc("/api/patch", s.guard(s.handlePatch))
	s.mux.HandleFunc("/api/browse", s.guard(s.handleBrowse))
	s.mux.HandleFunc("/api/folder", s.guard(s.handleAddFolder))

	url := fmt.Sprintf("http://%s/?t=%s", browsableAddr(s.addr), s.token)
	fmt.Println("kh3save UI:", url)
	if isLoopback(s.addr) {
		fmt.Println("This address is local to this machine and the token changes every run.")
	} else {
		fmt.Printf("WARNING: listening on %s, which is not loopback. Anything that can\n", s.addr)
		fmt.Println("reach that port can reach your saves if it also has the token above.")
		fmt.Println("Publish it to 127.0.0.1 only, and never onto an untrusted network.")
	}
	fmt.Println("Press Ctrl-C to stop.")
	if !noBrowser {
		openBrowser(url)
	}
	srv := &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.Serve(ln)
}

// isLoopback reports whether the bound address is one a remote host cannot
// reach. An unspecified address (0.0.0.0, ::) is reachable, so it is not.
func isLoopback(addr string) bool {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// browsableAddr turns a wildcard bind into something that can be pasted into a
// browser: 0.0.0.0:8787 is a valid thing to listen on and not a valid thing to
// connect to, and in the container case the reachable name is loopback anyway.
//
// It answers 127.0.0.1 for either wildcard, deliberately. Go reports a bind to
// 0.0.0.0 as [::] on a dual-stack host, and such a listener accepts IPv4 all
// the same, whereas `docker run -p` publishes IPv4 by default -- so printing
// the v6 form for a v6 wildcard would hand out a URL that does not connect.
func browsableAddr(addr string) string {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if ip := net.ParseIP(h); ip != nil && ip.IsUnspecified() {
		return net.JoinHostPort("127.0.0.1", p)
	}
	return addr
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("could not open a browser automatically; paste the address above")
	}
}

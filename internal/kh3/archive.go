package kh3

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Saves are routinely passed around as zip archives of the whole
//
//	KINGDOM HEARTS III/<platform>/<account>/SaveGames/kh3sv2/data
//
// tree: that is what a backup looks like, and what people attach to a forum
// post or hand to someone else. This file lets one of those archives be used
// anywhere a save folder can be used, without unpacking it first.
//
// A save inside an archive is addressed by a single path string, the way jar:
// URLs do it:
//
//	/backups/KH3_CRIT.zip!KINGDOM HEARTS III/Steam/765.../KHIII_slot0.bin
//
// so every command that already takes a save path keeps working, and the
// account id is still discovered from the numeric directory in the path.

// ArchiveSep marks the boundary between an archive and a file inside it.
const ArchiveSep = "!"

// maxMemberSize caps what will be pulled out of an archive. A real slot is
// about 9.7 MB, so this leaves generous room while refusing to let a crafted
// zip exhaust memory. That matters because the web UI takes a path from the
// browser and hands it straight to ReadFile.
const maxMemberSize = 64 << 20

// maxContainerSize bounds a whole file read into memory for a backup.
const maxContainerSize = 512 << 20

// maxArchiveEntries bounds how many members are walked. A save archive holds a
// handful; a zip declaring millions costs header memory for nothing.
const maxArchiveEntries = 10000

// SplitArchive splits an in-archive path into the archive and the member.
// It reports false for an ordinary path, which is left untouched.
//
// The separator is only honoured where the left-hand side ends in ".zip", so
// a directory or file with a "!" in its name is not mistaken for an archive.
func SplitArchive(p string) (archive, member string, ok bool) {
	for i := 0; i < len(p); i++ {
		if p[i] != ArchiveSep[0] {
			continue
		}
		if strings.HasSuffix(strings.ToLower(p[:i]), ".zip") {
			return p[:i], p[i+1:], true
		}
	}
	return p, "", false
}

// JoinArchive builds the path of one member of one archive.
func JoinArchive(archive, member string) string {
	return archive + ArchiveSep + member
}

// InArchive reports whether p addresses a file inside an archive.
func InArchive(p string) bool {
	_, _, ok := SplitArchive(p)
	return ok
}

// IsArchive reports whether p is itself a zip archive on disk, as opposed to
// a save directory.
func IsArchive(p string) bool {
	if !strings.HasSuffix(strings.ToLower(p), ".zip") {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// Base names the save, not the archive it happens to live in.
func Base(p string) string {
	if _, member, ok := SplitArchive(p); ok {
		return path.Base(member)
	}
	return filepath.Base(p)
}

// Container returns the file on disk that holds p: the archive when p names a
// member, and p itself otherwise. It is what a backup has to copy and what an
// allowlist has to check.
func Container(p string) string {
	if archive, _, ok := SplitArchive(p); ok {
		return archive
	}
	return p
}

// ListArchiveSaves returns the KHIII_*.bin members of a zip archive, as paths
// that ReadFile and WriteFile understand.
func ListArchiveSaves(archive string) ([]string, error) {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", archive, err)
	}
	defer r.Close()

	var out []string
	if n := len(r.File); n > maxArchiveEntries {
		return nil, fmt.Errorf("archive declares %d entries, more than the %d limit", n, maxArchiveEntries)
	}
	for _, f := range r.File {
		// Zip member names always use forward slashes, whatever the OS.
		if strings.HasSuffix(f.Name, "/") || !isSaveName(path.Base(f.Name)) {
			continue
		}
		out = append(out, JoinArchive(archive, f.Name))
	}
	sort.Strings(out)
	return out, nil
}

// ListSaves returns the saves held by p, which may be a directory or a zip.
func ListSaves(p string) []string {
	if IsArchive(p) {
		out, _ := ListArchiveSaves(p)
		return out
	}
	return ListSaveFiles(p)
}

// ReadFile reads a save from disk or from inside an archive.
func ReadFile(p string) ([]byte, error) {
	archive, member, ok := SplitArchive(p)
	if !ok {
		return os.ReadFile(p)
	}
	r, err := zip.OpenReader(archive)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", archive, err)
	}
	defer r.Close()

	f := findMember(r.File, member)
	if f == nil {
		return nil, fmt.Errorf("%s: no %q inside the archive", archive, member)
	}
	if f.UncompressedSize64 > maxMemberSize {
		return nil, fmt.Errorf("%s: %s is %d bytes, larger than the %d byte limit",
			archive, member, f.UncompressedSize64, maxMemberSize)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	defer rc.Close()

	// The declared size above is only a claim by the archive, so cap the read
	// itself as well and treat an overrun as a bad archive.
	data, err := io.ReadAll(io.LimitReader(rc, maxMemberSize+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if len(data) > maxMemberSize {
		return nil, fmt.Errorf("%s: %s unpacks to more than the %d byte limit",
			archive, member, maxMemberSize)
	}
	return data, nil
}

// WriteFile writes a save to disk, or replaces it inside an archive.
func WriteFile(p string, data []byte, perm fs.FileMode) error {
	archive, member, ok := SplitArchive(p)
	if !ok {
		return os.WriteFile(p, data, perm)
	}
	return replaceMember(archive, member, data)
}

// BackupOf copies whatever file p lives in (the save itself, or the whole
// archive when p names a member) to a timestamped name beside it, and
// returns that name. Replacing one member rewrites the entire zip, so for an
// archive the entire zip is what has to be recoverable.
func BackupOf(p string, now time.Time) (string, error) {
	src := Container(p)
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	// A save is ~9.7 MB and an archive of a few is well under this. Refuse
	// rather than read something enormous into memory because it happened to
	// sit at the path we were given.
	if info.Size() > maxContainerSize {
		return "", fmt.Errorf("%s is %d bytes, larger than the %d byte limit for a backup",
			src, info.Size(), int64(maxContainerSize))
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}

	base := fmt.Sprintf("%s.bak.%s", src, now.Format("20060102-150405"))
	// An archive keeps its extension last, so the backup is still openable as
	// a zip: this tool recognises an archive by its suffix, and so does every
	// file manager. Without it "backup.zip.bak.20260905-170442" is a file
	// nothing will open, which is a poor thing for a backup to be.
	ext := ""
	if e := filepath.Ext(src); strings.EqualFold(e, ".zip") {
		ext = e
	}

	// The timestamp is only second-resolution, so two saves in the same second
	// would silently overwrite the first backup. O_EXCL turns that collision
	// into an error we can step around instead of data we quietly destroyed.
	// Preserve the source mode too: a backup of a private save must not be
	// created world-readable.
	mode := info.Mode().Perm()
	for i := 0; i < 100; i++ {
		dst := base + ext
		if i > 0 {
			dst = fmt.Sprintf("%s-%d%s", base, i, ext)
		}
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			os.Remove(dst)
			return "", err
		}
		if err := f.Close(); err != nil {
			os.Remove(dst)
			return "", err
		}
		return dst, nil
	}
	return "", fmt.Errorf("could not find an unused backup name beside %s", src)
}

func findMember(files []*zip.File, name string) *zip.File {
	for _, f := range files {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// archiveWrite serialises archive rewrites. The web UI can have two swaps in
// flight at once, and two goroutines rewriting one zip would race to rename
// over each other, losing one of the edits entirely.
var archiveWrite sync.Mutex

// replaceMember rewrites the archive with one member's contents replaced.
// A zip cannot be edited in place, so this streams every other entry across
// unchanged (still compressed, never re-encoded) into a temporary file
// next to the original, then renames it over the top.
func replaceMember(archive, member string, data []byte) error {
	archiveWrite.Lock()
	defer archiveWrite.Unlock()

	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(archive); err == nil {
		mode = fi.Mode().Perm()
	}

	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("%s: %w", archive, err)
	}
	closed := false
	defer func() {
		if !closed {
			r.Close()
		}
	}()
	if findMember(r.File, member) == nil {
		return fmt.Errorf("%s: no %q inside the archive", archive, member)
	}

	tmp, err := os.CreateTemp(filepath.Dir(archive), ".kh3save-*.zip")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	w := zip.NewWriter(tmp)
	if r.Comment != "" {
		if err := w.SetComment(r.Comment); err != nil {
			return err
		}
	}
	for _, f := range r.File {
		if f.Name != member {
			// Copy moves the compressed bytes straight across, so untouched
			// members are neither decompressed nor recompressed.
			if err := w.Copy(f); err != nil {
				return fmt.Errorf("%s: copying %s: %w", archive, f.Name, err)
			}
			continue
		}
		h := f.FileHeader
		h.Method = zip.Deflate
		// The writer recomputes these; carrying the old values across would
		// describe the old contents.
		h.CRC32, h.CompressedSize, h.UncompressedSize = 0, 0, 0
		h.CompressedSize64, h.UncompressedSize64 = 0, 0
		dst, err := w.CreateHeader(&h)
		if err != nil {
			return err
		}
		if _, err := dst.Write(data); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil && !os.IsPermission(err) {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Windows will not rename over a file that is still open, so let go of the
	// original before replacing it.
	closed = true
	if err := r.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, archive); err != nil {
		return err
	}
	done = true
	return nil
}

// DescribeArchive reports what a backup zip holds, for display beside the real
// save folders. The layout inside is the game's own, so the platform and the
// account are read back out of the member path:
//
//	KINGDOM HEARTS III/<platform>/<account>/SaveGames/kh3sv2/data/KHIII_*.bin
//
// Cloud is deliberately left false: the Steam Cloud warning is about the game
// restoring its own copy over an edit, which cannot happen to an archive.
func DescribeArchive(archive string) SaveDir {
	d := SaveDir{Path: archive, Platform: "zip", Archive: true}
	members, err := ListArchiveSaves(archive)
	if err != nil || len(members) == 0 {
		return d
	}
	_, member, _ := SplitArchive(members[0])
	parts := strings.Split(path.Dir(member), "/")
	if n := len(parts); n >= 5 {
		d.AccountID = parts[n-4]
		d.Platform = parts[n-5]
	}
	return d
}

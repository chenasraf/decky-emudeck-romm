package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // matches the code under test
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

type member struct{ name, data string }

func zipOf(t *testing.T, members ...member) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, m := range members {
		f, err := w.Create(m.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(m.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serve makes the server's only game file of ROM id a file named name holding data.
func (f *downloadFixture) serve(id int, name string, data []byte) {
	f.server.mu.Lock()
	defer f.server.mu.Unlock()
	fileID := id * 10
	f.server.files[id] = []domain.ROMFile{{ID: fileID, Name: name, SizeBytes: int64(len(data)), Category: "game"}}
	f.server.content[fileID] = data
}

// installed downloads ROM id and fails the test unless it installs.
func (f *downloadFixture) installed(t *testing.T, id int) domain.OwnedROM {
	t.Helper()
	if _, err := f.downloads.Queue(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if u := f.end(t, id); u.State != domain.StateInstalled {
		t.Fatalf("end: %+v", u)
	}
	f.stagingEmpty(t)
	rec, _, _ := f.owned.Owned(context.Background(), id)
	return rec
}

// listDir returns the paths of the files under dir, relative to it.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out = append(out, rel)
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s)) //nolint:gosec // matches the code under test
	return hex.EncodeToString(sum[:])
}

func TestDiscArchiveIsExtracted(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serve(5, "Ridge Racer.zip", zipOf(t,
		member{"Ridge Racer.cue", "FILE \"Ridge Racer.bin\" BINARY"},
		member{"Ridge Racer.bin", "disc bytes"},
	))
	rec := f.installed(t, 5)

	psx := filepath.Join(f.root, "roms", "psx")
	if got := listDir(t, psx); len(got) != 2 || got[0] != "Ridge Racer.bin" || got[1] != "Ridge Racer.cue" {
		t.Fatalf("psx holds %v", got)
	}
	// The record lists what was extracted, hashed as extracted, not the archive.
	if len(rec.Files) != 2 {
		t.Fatalf("record %+v", rec.Files)
	}
	for _, p := range rec.Files {
		data, _ := os.ReadFile(p.Path)
		if p.SHA1 != sha1Hex(string(data)) || p.SizeBytes != int64(len(data)) {
			t.Fatalf("recorded %+v for %q", p, data)
		}
	}

	// Deleting it removes every extracted file.
	if _, err := f.downloads.Delete(context.Background(), []int{5}, 0); err != nil {
		t.Fatal(err)
	}
	<-f.deletes
	if u := <-f.deletes; u.Problem != nil {
		t.Fatalf("delete: %v", u.Problem)
	}
	if got := listDir(t, psx); len(got) != 0 {
		t.Fatalf("left %v", got)
	}
}

func TestNestedArchiveFoldersGoWithDelete(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serve(5, "Ridge Racer.zip", zipOf(t, member{"Ridge Racer/Disc.cue", "cue"}, member{"Ridge Racer/Disc.bin", "bin"}))
	f.installed(t, 5)
	psx := filepath.Join(f.root, "roms", "psx")
	if got := listDir(t, psx); len(got) != 2 || got[0] != filepath.Join("Ridge Racer", "Disc.bin") {
		t.Fatalf("psx holds %v", got)
	}

	if _, err := f.downloads.Delete(context.Background(), []int{5}, 0); err != nil {
		t.Fatal(err)
	}
	<-f.deletes
	<-f.deletes
	entries, err := os.ReadDir(psx)
	if err != nil || len(entries) != 0 {
		t.Fatalf("psx holds %v, %v", entries, err)
	}
}

func TestArcadeArchiveStaysZipped(t *testing.T) {
	f := newDownloadFixture(t, 2)
	data := zipOf(t, member{"sf2.u1", "a"}, member{"sf2.u2", "b"})
	f.serve(6, "sf2.zip", data)
	rec := f.installed(t, 6)
	if got := listDir(t, filepath.Join(f.root, "roms", "arcade")); len(got) != 1 || got[0] != "sf2.zip" {
		t.Fatalf("arcade holds %v", got)
	}
	if len(rec.Files) != 1 || rec.Files[0].SHA1 != sha1Hex(string(data)) {
		t.Fatalf("record %+v", rec.Files)
	}
}

func TestKeepArchivesToggle(t *testing.T) {
	ctx := context.Background()
	single := zipOf(t, member{"Zipped (USA).sfc", "cart"})

	t.Run("on keeps a single-file zip", func(t *testing.T) {
		f := newDownloadFixture(t, 2)
		f.serve(7, "Zipped.zip", single)
		f.installed(t, 7)
		if got := listDir(t, f.snes); len(got) != 1 || got[0] != "Zipped.zip" {
			t.Fatalf("snes holds %v", got)
		}
	})

	t.Run("off extracts it", func(t *testing.T) {
		f := newDownloadFixture(t, 2)
		if err := f.downloads.SetKeepArchives(ctx, 1, false); err != nil {
			t.Fatal(err)
		}
		if policy, keep := f.downloads.Archives(ctx, 1); policy != domain.ArchivesOptional || keep {
			t.Fatalf("archives %s, keep %v", policy, keep)
		}
		f.serve(7, "Zipped.zip", single)
		f.installed(t, 7)
		if got := listDir(t, f.snes); len(got) != 1 || got[0] != "Zipped (USA).sfc" {
			t.Fatalf("snes holds %v", got)
		}
	})

	t.Run("on extracts several files", func(t *testing.T) {
		f := newDownloadFixture(t, 2)
		f.serve(7, "Zipped.zip", zipOf(t, member{"a.sfc", "a"}, member{"b.sfc", "b"}))
		f.installed(t, 7)
		if got := listDir(t, f.snes); len(got) != 2 {
			t.Fatalf("snes holds %v", got)
		}
	})

	t.Run("only where it applies", func(t *testing.T) {
		f := newDownloadFixture(t, 2)
		for _, platform := range []int{3, 4} {
			if err := f.downloads.SetKeepArchives(ctx, platform, false); kindOf(err) != domain.KindInvalidInput {
				t.Fatalf("platform %d: %v", platform, err)
			}
		}
	})
}

func TestSevenZipIsExtracted(t *testing.T) {
	f := newDownloadFixture(t, 2)
	data, err := os.ReadFile(filepath.Join("testdata", "foo-bar.7z"))
	if err != nil {
		t.Fatal(err)
	}
	f.serve(5, "Ridge Racer.7z", data)
	rec := f.installed(t, 5)
	if got := listDir(t, filepath.Join(f.root, "roms", "psx")); len(got) != 2 || got[0] != "bar" || got[1] != "foo" {
		t.Fatalf("psx holds %v", got)
	}
	if rec.Files[0].SHA1 != sha1Hex("bar\n") {
		t.Fatalf("record %+v", rec.Files)
	}
}

func TestArchiveRefusals(t *testing.T) {
	for name, data := range map[string]func(t *testing.T) []byte{
		"escaping path": func(t *testing.T) []byte { return zipOf(t, member{"../evil.bin", "x"}) },
		"empty":         func(t *testing.T) []byte { return zipOf(t) },
		"corrupt":       func(*testing.T) []byte { return []byte("not a zip at all") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newDownloadFixture(t, 2)
			f.serve(5, "Ridge Racer.zip", data(t))
			if _, err := f.downloads.Queue(context.Background(), 5); err != nil {
				t.Fatal(err)
			}
			if u := f.end(t, 5); u.State != domain.StateFailed || kindOf(u.Problem) != domain.KindCantDownload {
				t.Fatalf("end: %+v", u)
			}
			if got := listDir(t, filepath.Join(f.root, "roms")); len(got) != 0 {
				t.Fatalf("placed %v", got)
			}
		})
	}
}

func TestExtractedFileNeverOverwrites(t *testing.T) {
	f := newDownloadFixture(t, 2)
	psx := filepath.Join(f.root, "roms", "psx")
	if err := os.MkdirAll(psx, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(psx, "Ridge Racer.bin"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.serve(5, "Ridge Racer.zip", zipOf(t, member{"Ridge Racer.cue", "cue"}, member{"Ridge Racer.bin", "bin"}))
	if _, err := f.downloads.Queue(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if u := f.end(t, 5); kindOf(u.Problem) != domain.KindTargetExists {
		t.Fatalf("end: %+v", u)
	}
	// Neither file moved in: the cue isn't left behind without its bin.
	if got := listDir(t, psx); len(got) != 1 {
		t.Fatalf("psx holds %v", got)
	}
	if data, _ := os.ReadFile(filepath.Join(psx, "Ridge Racer.bin")); string(data) != "mine" {
		t.Fatalf("replaced: %q", data)
	}
}

func TestArchiveSpaceCountsTwice(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	// Ridge Racer's catalog size is 1000: one copy fits, the archive and its contents don't.
	f.free.Store(1000 + spaceMargin)
	if _, err := f.downloads.Queue(ctx, 5); kindOf(err) != domain.KindNoSpace {
		t.Fatalf("want no_space, got %v", err)
	}
	f.free.Store(2000 + spaceMargin)
	if _, err := f.downloads.Queue(ctx, 5); err != nil {
		t.Fatal(err)
	}
}

func TestPlacesAcrossDrives(t *testing.T) {
	f := newDownloadFixture(t, 2)
	// The PlayStation folder is a link to an SD card: renames into it fail.
	psx := filepath.Join(f.root, "roms", "psx")
	rename = func(from, to string) error {
		if strings.HasPrefix(to, psx+string(filepath.Separator)) {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { rename = os.Rename })

	f.serve(5, "Ridge Racer.zip", zipOf(t, member{"Ridge Racer.cue", "cue"}, member{"Ridge Racer.bin", "disc bytes"}))
	f.installed(t, 5)
	if got := listDir(t, psx); len(got) != 2 {
		t.Fatalf("psx holds %v", got)
	}
	if data, _ := os.ReadFile(filepath.Join(psx, "Ridge Racer.bin")); string(data) != "disc bytes" {
		t.Fatalf("copied %q", data)
	}
}

func TestSpaceIsMeasuredWhereTheGameGoes(t *testing.T) {
	f := newDownloadFixture(t, 2)
	// Staging has room, but the PlayStation folder is on a full SD card.
	psx := filepath.Join(f.root, "roms", "psx")
	if err := os.MkdirAll(psx, 0o755); err != nil {
		t.Fatal(err)
	}
	f.drives.Store(psx, int64(10))
	_, err := f.downloads.Queue(context.Background(), 5)
	if kindOf(err) != domain.KindNoSpace || !strings.Contains(err.Error(), psx) {
		t.Fatalf("want no_space naming %s, got %v", psx, err)
	}
}

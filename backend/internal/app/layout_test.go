package app

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// gameFile is a file of a game on the fake server: where it sits inside the game, its category
// and its bytes.
type gameFile struct {
	path, category, data string
}

// serveSet makes the server's files of ROM id the files given, numbering them from id*100.
func (f *downloadFixture) serveSet(id int, files ...gameFile) {
	f.server.mu.Lock()
	defer f.server.mu.Unlock()
	var list []domain.ROMFile
	for i, g := range files {
		fileID := id*100 + i
		list = append(list, domain.ROMFile{ID: fileID, Name: filepath.Base(g.path), Path: g.path,
			SizeBytes: int64(len(g.data)), Category: g.category})
		f.server.content[fileID] = []byte(g.data)
	}
	f.server.files[id] = list
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestMultiDiscPlaylist(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serveSet(8,
		gameFile{"Final Fantasy VII (USA) (Disc 2).cue", "game", "cue 2"},
		gameFile{"Final Fantasy VII (USA) (Disc 2).bin", "game", "bin 2"},
		gameFile{"Final Fantasy VII (USA) (Disc 1).cue", "game", "cue 1"},
		gameFile{"Final Fantasy VII (USA) (Disc 1).bin", "game", "bin 1"},
		gameFile{"manual/Manual.pdf", "manual", "pdf"},
	)
	rec := f.installed(t, 8)

	psx := filepath.Join(f.root, "roms", "psx")
	folder := "Final Fantasy VII (USA).m3u"
	want := []string{
		filepath.Join(folder, "Final Fantasy VII (USA) (Disc 1).bin"),
		filepath.Join(folder, "Final Fantasy VII (USA) (Disc 1).cue"),
		filepath.Join(folder, "Final Fantasy VII (USA) (Disc 2).bin"),
		filepath.Join(folder, "Final Fantasy VII (USA) (Disc 2).cue"),
		filepath.Join(folder, "Final Fantasy VII (USA).m3u"),
	}
	if got := listDir(t, psx); !slices.Equal(got, want) {
		t.Fatalf("psx holds %v", got)
	}
	if got := readFile(t, filepath.Join(psx, folder, folder)); got != "Final Fantasy VII (USA) (Disc 1).cue\nFinal Fantasy VII (USA) (Disc 2).cue\n" {
		t.Fatalf("playlist %q", got)
	}
	// The manual was never asked for.
	if n := len(f.server.froms); n != 4 {
		t.Fatalf("%d files downloaded", n)
	}
	// The record covers every file, the playlist too, and snapshots every downloaded one.
	if len(rec.Files) != 5 || len(rec.Snapshot) != 4 {
		t.Fatalf("record: %d files, %d snapshots", len(rec.Files), len(rec.Snapshot))
	}
	for _, p := range rec.Files {
		if p.SHA1 != sha1Hex(readFile(t, p.Path)) {
			t.Fatalf("recorded %+v", p)
		}
	}

	// Deleting takes the whole folder.
	if _, err := f.downloads.Delete(context.Background(), []int{8}, 0); err != nil {
		t.Fatal(err)
	}
	<-f.deletes
	if u := <-f.deletes; u.Problem != nil {
		t.Fatalf("delete: %v", u.Problem)
	}
	if entries, _ := os.ReadDir(psx); len(entries) != 0 {
		t.Fatalf("psx holds %v", entries)
	}
}

func TestGDISet(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serveSet(9,
		gameFile{"disc.gdi", "game", "gdi"},
		gameFile{"track01.bin", "game", "t1"},
		gameFile{"track02.raw", "game", "t2"},
		gameFile{"track03.bin", "game", "t3"},
	)
	f.installed(t, 9)
	dc := filepath.Join(f.root, "roms", "dreamcast", "Shenmue (USA).m3u")
	if got := readFile(t, filepath.Join(dc, "Shenmue (USA).m3u")); got != "disc.gdi\n" {
		t.Fatalf("playlist %q", got)
	}
	if got := readFile(t, filepath.Join(dc, "track02.raw")); got != "t2" {
		t.Fatalf("track %q", got)
	}
}

func TestDOSFolderIsZipped(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serveSet(10, gameFile{"DOOM.EXE", "game", "exe"}, gameFile{"DATA/DOOM1.WAD", "game", "wad"})
	rec := f.installed(t, 10)

	path := filepath.Join(f.root, "roms", "dos", "Doom.zip")
	if got := listDir(t, filepath.Dir(path)); len(got) != 1 || got[0] != "Doom.zip" {
		t.Fatalf("dos holds %v", got)
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var names []string
	for _, zf := range r.File {
		names = append(names, zf.Name)
	}
	if !slices.Equal(names, []string{"DOOM.EXE", "DATA/DOOM1.WAD"}) {
		t.Fatalf("zip holds %v", names)
	}
	if len(rec.Files) != 1 || rec.Files[0].SHA1 != sha1Hex(readFile(t, path)) {
		t.Fatalf("record %+v", rec.Files)
	}
}

func TestPS3Folder(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serveSet(11,
		gameFile{"PS3_GAME/PARAM.SFO", "", "sfo"},
		gameFile{"PS3_GAME/USRDIR/EBOOT.BIN", "", "eboot"},
		gameFile{"update/patch.pkg", "update", "pkg"},
	)
	f.installed(t, 11)
	ps3 := filepath.Join(f.root, "roms", "ps3")
	want := []string{
		filepath.Join("Demon's Souls.ps3", "PS3_GAME", "PARAM.SFO"),
		filepath.Join("Demon's Souls.ps3", "PS3_GAME", "USRDIR", "EBOOT.BIN"),
	}
	if got := listDir(t, ps3); !slices.Equal(got, want) {
		t.Fatalf("ps3 holds %v", got)
	}
}

func TestSetResumesFileByFile(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serveSet(8,
		gameFile{"Game (Disc 1).chd", "game", "first disc bytes"},
		gameFile{"Game (Disc 2).chd", "game", "second disc bytes"},
	)
	// The second disc drops partway through once.
	f.server.drops[801] = 1
	f.installed(t, 8)
	// Disc 1, then disc 2 cut off, then disc 2 again from where it stopped: disc 1 isn't fetched
	// twice.
	froms := f.server.froms
	if len(froms) != 3 || froms[0] != 0 || froms[1] != 0 || froms[2] == 0 {
		t.Fatalf("froms %v", froms)
	}
}

func TestSetNeverMergesIntoAFolder(t *testing.T) {
	f := newDownloadFixture(t, 2)
	folder := filepath.Join(f.root, "roms", "psx", "Final Fantasy VII (USA).m3u")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	f.serveSet(8, gameFile{"a (Disc 1).chd", "game", "a"}, gameFile{"a (Disc 2).chd", "game", "b"})
	if _, err := f.downloads.Queue(context.Background(), 8); kindOf(err) != domain.KindTargetExists {
		t.Fatalf("queue: %v", err)
	}
	if len(f.server.froms) != 0 {
		t.Fatal("downloaded a game it couldn't place")
	}
}

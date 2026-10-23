package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

var rmg = domain.GameLauncher{
	Label: "Rosalie's Mupen GUI (Standalone)", Command: "%EMULATOR_ROSALIES-MUPEN-GUI% --nogui -q %ROM%", Known: true}

// n64ROM is a small ROM in .z64 byte order with name in its header.
func n64ROM(name string) []byte {
	z := make([]byte, 0x1000)
	copy(z, []byte{0x80, 0x37, 0x12, 0x40})
	copy(z[0x20:], name)
	return z
}

// rmgGame records Paper Mario, with ROM id 60, as downloaded by the plugin and played in RMG, and
// returns the path, without the extension, of the files RMG keeps its save in.
func (f *saveFixture) rmgGame(t *testing.T) string {
	t.Helper()
	f.launchers["n64"] = rmg
	rom := n64ROM("PAPER MARIO")
	parsed, err := domain.ParseN64ROM(rom)
	if err != nil {
		t.Fatal(err)
	}
	f.n64db[parsed.MD5] = "Paper Mario (U) [!]"
	f.saves = f.newSaves()
	path := filepath.Join(f.root, "roms", "n64", "Paper Mario (USA).z64")
	writeFile(t, path, rom)
	f.catalog.mu.Lock()
	f.catalog.roms[60] = domain.ROM{ID: 60, PlatformID: 11, Name: "Paper Mario", FileName: "Paper Mario (USA).z64"}
	f.catalog.mu.Unlock()
	if err := f.owned.SaveOwned(context.Background(), domain.OwnedROM{ROMID: 60, PlatformID: 11, Files: []domain.PlacedFile{{Path: path}}}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(f.root, "saves", "RMG", "saves", "Paper Mario (U) [!]-"+parsed.MD5[:8])
}

// eeprom is a 4 Kbit EEPROM save starting with data.
func eeprom(data string) []byte {
	out := bytes.Repeat([]byte{0xFF}, 0x200)
	copy(out, data)
	return out
}

func zipMembers(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		out[f.Name], _ = io.ReadAll(rc)
		_ = rc.Close()
	}
	return out
}

func (f *saveFixture) syncGame(t *testing.T, id int) SaveSummary {
	t.Helper()
	s, err := f.saves.Sync(context.Background(), []int{id})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRMGSaveUploadsAsTheSharedUnit(t *testing.T) {
	f := newSaveFixture(t)
	base := f.rmgGame(t)
	if st := f.statusOf(t, 60); !st.Syncs || st.Path != base+".zip" || st.Emulator != "rmg" || st.Saver != "RMG" {
		t.Fatalf("status %+v", st)
	}
	writeFile(t, base+".eep", eeprom("mario"))

	if s := f.syncGame(t, 60); s.Uploaded != 1 {
		t.Fatalf("summary %+v", s)
	}
	head, _ := f.romm.head(60)
	members := zipMembers(t, head.data)
	if len(members) != 1 || !bytes.Equal(members["eeprom"], eeprom("mario")) || head.Emulator != "rmg" ||
		head.FileName != filepath.Base(base)+".zip" {
		t.Fatalf("RomM holds %s %v", head.FileName, len(members))
	}
	// Syncing again does nothing: the unit hashes the same as RomM keeps it.
	if s := f.syncGame(t, 60); s.Uploaded+s.Downloaded+s.Conflicts != 0 {
		t.Fatalf("second sync %+v", s)
	}
}

func TestRMGDownloadsIntoItsFiles(t *testing.T) {
	cases := map[string]struct {
		emulator string
		data     func() []byte
	}{
		"another device's unit": {"m64plus_fz", func() []byte {
			s := &domain.N64Save{}
			s.TakeFile(".eep", eeprom("from fz"))
			return s.Unit()
		}},
		"RetroArch's .srm": {"mupen64plus_next", func() []byte {
			blob := bytes.Repeat([]byte{0xFF}, 0x48800)
			copy(blob, "from fz")
			return blob
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSaveFixture(t)
			base := f.rmgGame(t)
			f.romm.otherDevice(60, c.emulator, c.data())

			if s := f.syncGame(t, 60); s.Downloaded != 1 {
				t.Fatalf("summary %+v", s)
			}
			got := readFile(t, base+".eep")
			if len(got) != 0x800 || !strings.HasPrefix(got, "from fz") {
				t.Fatalf(".eep holds %d bytes: %q", len(got), got[:7])
			}
			gone(t, base+".sra")
			// What the Deck now holds reads back as the same save, whatever form RomM keeps it in.
			if s := f.syncGame(t, 60); s.Uploaded+s.Downloaded+s.Conflicts != 0 {
				t.Fatalf("second sync %+v", s)
			}
		})
	}
}

// A save kept in several files goes to RomM as one zip and comes back into the same files.
func TestRMGSeveralFilesRoundTrip(t *testing.T) {
	f := newSaveFixture(t)
	base := f.rmgGame(t)
	pak := bytes.Repeat([]byte{0x00}, 0x8000)
	for at := 0x10A; at < 0x200; at += 2 {
		binary.BigEndian.PutUint16(pak[at:], 0x0003)
	}
	binary.BigEndian.PutUint16(pak[0x10A:], 0x0005)
	copy(pak[0x300:], "pak note")
	writeFile(t, base+".eep", eeprom("mario"))
	writeFile(t, base+".mpk", pak)

	if s := f.syncGame(t, 60); s.Uploaded != 1 {
		t.Fatalf("upload %+v", s)
	}
	head, _ := f.romm.head(60)
	if members := zipMembers(t, head.data); len(members) != 2 || !bytes.Equal(members["eeprom"], eeprom("mario")) || !bytes.Equal(members["pak1"], pak) {
		t.Fatalf("RomM holds %d members", len(members))
	}

	for _, ext := range []string{".eep", ".mpk"} {
		if err := os.Remove(base + ext); err != nil {
			t.Fatal(err)
		}
	}
	f.romm.otherDevice(60, "rmg", head.data)
	if s := f.syncGame(t, 60); s.Downloaded != 1 {
		t.Fatalf("download %+v", s)
	}
	// RMG keeps an EEPROM at 16 Kbit whichever size the game uses.
	if got := readFile(t, base+".eep"); len(got) != 0x800 || got[:0x200] != string(eeprom("mario")) {
		t.Fatalf(".eep holds %d bytes", len(got))
	}
	if got := readFile(t, base+".mpk"); len(got) != 4*0x8000 || got[:0x8000] != string(pak) {
		t.Fatalf(".mpk holds %d bytes", len(got))
	}
}

// A save RMG made under another name, before a database update say, is found by the ROM's hash.
func TestRMGFindsSavesByHashAlone(t *testing.T) {
	f := newSaveFixture(t)
	base := f.rmgGame(t)
	other := filepath.Join(filepath.Dir(base), "PAPER MARIO"+base[strings.LastIndex(base, "-"):])
	writeFile(t, other+".sra", bytes.Repeat([]byte{0x11}, 0x8000))
	if st := f.statusOf(t, 60); st.Path != other+".zip" {
		t.Fatalf("status %+v", st)
	}
}

type brokenN64Database struct{}

func (brokenN64Database) GoodName(domain.N64ROM) (string, bool, error) {
	return "", false, errors.New("RMG isn't installed")
}

// Without RMG's database a save that isn't there yet can't be named, and the game doesn't sync.
func TestRMGWithoutItsDatabase(t *testing.T) {
	f := newSaveFixture(t)
	f.rmgGame(t)
	d := f.downloads
	f.saves = NewSaves(context.Background(), d.catalog, d.install, d.conn, func(domain.Server, string) SaveServer { return f.romm },
		f.owned, f.store, nil, SaveSources{Launchers: f.launchers, Configs: f.configs, N64: brokenN64Database{}},
		func() (domain.Emulator, bool) { return domain.Emulator{}, false }, f.backups, func(SaveSyncEvent) {}, f.saves.log)
	if st := f.statusOf(t, 60); st.Syncs || !strings.Contains(st.Why, "database") {
		t.Fatalf("status %+v", st)
	}
}

// A write to any of RMG's files of the game syncs it.
func TestRMGWriteSyncsTheGame(t *testing.T) {
	f := newSaveFixture(t)
	base := f.rmgGame(t)
	writeFile(t, base+".mpk", nil)
	f.saves.saveWritten(context.Background(), base+".mpk")
	f.ended(t)
}

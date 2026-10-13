package app

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestConvertedOnTheServer(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serve(12, "Zelda.cia", []byte("stored cia bytes"))
	f.server.converts[120] = conversion{name: "Zelda.cci", data: []byte("converted cci bytes")}
	rec := f.installed(t, 12)

	n3ds := filepath.Join(f.root, "roms", "n3ds")
	if got := listDir(t, n3ds); len(got) != 1 || got[0] != "Zelda.cci" {
		t.Fatalf("n3ds holds %v", got)
	}
	if got := readFile(t, filepath.Join(n3ds, "Zelda.cci")); got != "converted cci bytes" {
		t.Fatalf("placed %q", got)
	}
	if !slices.Equal(f.server.formats, []string{"cci"}) || len(f.server.froms) != 0 {
		t.Fatalf("asked for %v, downloaded stored %v", f.server.formats, f.server.froms)
	}
	// The record hashes what was placed, and snapshots the file as RomM keeps it.
	if rec.Files[0].SHA1 != sha1Hex("converted cci bytes") || rec.Snapshot[0].FileID != 120 ||
		rec.Snapshot[0].SizeBytes != int64(len("stored cia bytes")) {
		t.Fatalf("record %+v", rec)
	}
}

func TestConvertingIsReported(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serve(12, "Zelda.cia", []byte("stored"))
	f.server.converts[120] = conversion{name: "Zelda.cci", data: []byte("converted")}
	if _, err := f.downloads.Queue(context.Background(), 12); err != nil {
		t.Fatal(err)
	}
	var converting bool
	deadline := time.After(3 * time.Second)
	for {
		select {
		case u := <-f.updates:
			converting = converting || u.Converting
			if u.State == domain.StateInstalled {
				if !converting {
					t.Fatal("never said it was converting")
				}
				if u.Converting {
					t.Fatal("still converting once installed")
				}
				return
			}
		case <-deadline:
			t.Fatal("didn't install")
		}
	}
}

func TestStoredWhenTheServerWontConvert(t *testing.T) {
	for name, setup := range map[string]func(f *downloadFixture){
		"refused":             func(f *downloadFixture) { f.server.converts[120] = conversion{refuse: true} },
		"predates converting": func(*downloadFixture) {},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDownloadFixture(t, 2)
			f.serve(12, "Zelda.cia", []byte("stored cia bytes"))
			setup(f)
			f.installed(t, 12)
			if got := listDir(t, filepath.Join(f.root, "roms", "n3ds")); len(got) != 1 || got[0] != "Zelda.cia" {
				t.Fatalf("n3ds holds %v", got)
			}
			if len(f.server.convertedFroms) != 0 {
				t.Fatal("downloaded a conversion it was told isn't there")
			}
		})
	}
}

func TestPlayableFormatsAreNotConverted(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serve(1, "Chrono Trigger (USA).sfc", chrono)
	f.installed(t, 1)
	if len(f.server.formats) != 0 {
		t.Fatalf("asked to convert %v", f.server.formats)
	}
}

package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestInstallToPicksAnAltFolder(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()

	current, options := f.downloads.InstallTo(ctx, 4)
	if current != "arcade" || len(options) < 3 || !options[0].Primary || options[0].Folder != "arcade" || options[0].SRMSkips {
		t.Fatalf("install to %q, %+v", current, options)
	}
	skips := map[string]bool{}
	for _, o := range options {
		skips[o.Folder] = o.SRMSkips
	}
	// Steam ROM Manager has a parser for fbneo, not for fba.
	if skips["fbneo"] || !skips["fba"] {
		t.Fatalf("SRM skips %v", skips)
	}

	if err := f.downloads.SetInstallTo(ctx, 4, "fbneo"); err != nil {
		t.Fatal(err)
	}
	f.serve(6, "sf2.zip", zipOf(t, member{"sf2.u1", "a"}))
	rec := f.installed(t, 6)
	fbneo := filepath.Join(f.root, "roms", "fbneo")
	if got := listDir(t, fbneo); len(got) != 1 || got[0] != "sf2.zip" || rec.Files[0].Path != filepath.Join(fbneo, "sf2.zip") {
		t.Fatalf("fbneo holds %v, record %+v", got, rec.Files)
	}

	// Going back to the system's own folder leaves the game where it is.
	if err := f.downloads.SetInstallTo(ctx, 4, "arcade"); err != nil {
		t.Fatal(err)
	}
	if got := listDir(t, fbneo); len(got) != 1 {
		t.Fatalf("fbneo holds %v", got)
	}
	if current, _ := f.downloads.InstallTo(ctx, 4); current != "arcade" {
		t.Fatalf("install to %q", current)
	}

	if err := f.downloads.SetInstallTo(ctx, 4, "snes"); kindOf(err) != domain.KindInvalidInput {
		t.Fatalf("another system's folder: %v", err)
	}
	if _, options := f.downloads.InstallTo(ctx, 3); len(options) != 1 || options[0].Folder != "psx" {
		t.Fatalf("psx has alt folders %+v", options)
	}
}

func TestDeleteCleansTheFolderTheGameWentTo(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	if err := f.downloads.SetInstallTo(ctx, 1, "snesna"); err != nil {
		t.Fatal(err)
	}
	// Two files, so the zip is extracted into its own folder.
	f.serve(7, "Zipped.zip", zipOf(t, member{"Zipped/Zipped (USA).sfc", "cart"}, member{"Zipped/readme.txt", "hi"}))
	f.installed(t, 7)
	snesna := filepath.Join(f.root, "roms", "snesna")
	if got := listDir(t, snesna); len(got) != 2 {
		t.Fatalf("snesna holds %v", got)
	}
	if err := f.downloads.SetInstallTo(ctx, 1, "snes"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.downloads.Delete(ctx, []int{7}, 0); err != nil {
		t.Fatal(err)
	}
	<-f.deletes
	if u := <-f.deletes; u.Problem != nil {
		t.Fatalf("delete: %v", u.Problem)
	}
	// The game's folder goes too; snesna itself stays.
	entries, err := os.ReadDir(snesna)
	if err != nil || len(entries) != 0 {
		t.Fatalf("snesna holds %v, %v", entries, err)
	}
}

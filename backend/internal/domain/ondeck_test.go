package domain

import "testing"

func TestOccupant(t *testing.T) {
	folder := NewRomFolder([]string{
		"Golden Sun (USA).gba", "Metroid Fusion (USA).zip", "Crash (USA).cue", "Ridge Racer (USA).m3u",
		"Zelda.cci", "sf2.zip", "Mario Kart.sav",
	}, []string{"Doom v1.9", "Breath of Fire III (v1.1)", "Tekken 3.zip"})
	for _, tc := range []struct {
		name string
		slug string
		rom  ROM
		want string
	}{
		{"same name", "gba", ROM{FileName: "Golden Sun (USA).gba"}, "Golden Sun (USA).gba"},
		{"kept zipped by the user", "gba", ROM{FileName: "Metroid Fusion (USA).gba"}, "Metroid Fusion (USA).zip"},
		{"extracted from an archive", "psx", ROM{FileName: "Crash (USA).zip"}, "Crash (USA).cue"},
		{"several files in a playlist folder", "psx", ROM{FileName: "Ridge Racer (USA)", MultiFile: true}, "Ridge Racer (USA).m3u"},
		{"converted by the Server", "3ds", ROM{FileName: "Zelda.cia"}, "Zelda.cci"},
		{"a folder whose name has a dot", "dos", ROM{FileName: "Doom v1.9", MultiFile: true}, "Doom v1.9"},
		{"extracted into a folder whose name has a dot", "psx", ROM{FileName: "Breath of Fire III (v1.1).7z"}, "Breath of Fire III (v1.1)"},
		// A folder is no archive, whatever its name ends in.
		{"a folder named like an archive", "psx", ROM{FileName: "Tekken 3.cue"}, ""},
		{"no file name falls back to the name", "gba", ROM{Name: "Golden Sun (USA).gba"}, "Golden Sun (USA).gba"},
		// A file that keeps its name never matches another extension: a save beside the ROMs isn't the game.
		{"another file of the same stem", "snes", ROM{FileName: "Mario Kart.sfc"}, ""},
		{"an archive the system keeps", "arcade", ROM{FileName: "sf2.7z"}, "sf2.zip"},
		{"nothing there", "gba", ROM{FileName: "Advance Wars (USA).gba"}, ""},
	} {
		got, ok := folder.Occupant(system(t, tc.slug), tc.rom)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("%s: %q, %v; want %q", tc.name, got, ok, tc.want)
		}
	}
}

func TestROMFileMatches(t *testing.T) {
	sums := FileSums{CRC: "0012abcd", MD5: "aa", SHA1: "bb"}
	for _, tc := range []struct {
		file ROMFile
		want bool
	}{
		{ROMFile{SHA1: "BB", MD5: "zz"}, true},
		{ROMFile{SHA1: "cc", MD5: "aa"}, false},
		{ROMFile{MD5: "aa"}, true},
		{ROMFile{CRC: "12abcd"}, true},
		{ROMFile{SizeBytes: 10}, false},
	} {
		if got := tc.file.Matches(sums); got != tc.want {
			t.Errorf("%+v: %v", tc.file, got)
		}
	}
}

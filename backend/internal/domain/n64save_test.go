package domain

import (
	"archive/zip"
	"bytes"
	"testing"
)

// n64File is an emulator file of size bytes, blank (0xFF) but for data at offset.
func n64File(size, offset int, data ...byte) []byte {
	out := bytes.Repeat([]byte{0xFF}, size)
	copy(out[offset:], data)
	return out
}

func TestN64UnitFromMupenFiles(t *testing.T) {
	s := &N64Save{}
	s.TakeFile(".eep", n64File(0x200, 0, 1, 2, 3, 4))
	s.TakeFile(".sra", n64File(0x8000, 0, 0xA, 0xB, 0xC, 0xD))

	zr, err := zip.NewReader(bytes.NewReader(s.Unit()), int64(len(s.Unit())))
	if err != nil {
		t.Fatal(err)
	}
	members := map[string][]byte{}
	for _, f := range zr.File {
		data, _ := readZipFile(f)
		members[f.Name] = data
	}
	if len(members) != 2 || len(members["eeprom"]) != 0x200 || len(members["sram"]) != 0x8000 {
		t.Fatalf("members %v", len(members))
	}
	// SRAM goes into the unit in bus order: each 32-bit word reversed from mupen64plus's file.
	if got := members["sram"][:4]; !bytes.Equal(got, []byte{0xD, 0xC, 0xB, 0xA}) {
		t.Fatalf("sram starts %x", got)
	}
	if !bytes.Equal(members["eeprom"][:4], []byte{1, 2, 3, 4}) {
		t.Fatalf("eeprom starts %x", members["eeprom"][:4])
	}
}

func TestN64EEPROMSize(t *testing.T) {
	s := &N64Save{}
	s.TakeFile(".eep", n64File(0x800, 0x400, 7))
	if len(s.regions[n64RegionEEPROM]) != 0x800 {
		t.Fatalf("16 Kbit EEPROM kept %d bytes", len(s.regions[n64RegionEEPROM]))
	}
}

func TestN64UnusedTypesStayOut(t *testing.T) {
	s := &N64Save{}
	s.TakeFile(".fla", n64File(0x20000, 0))
	s.TakeFile(".mpk", bytes.Repeat(formattedPak(), 4))
	if !s.Empty() {
		t.Fatal("blank flash or formatted paks counted as a save")
	}
}

// What goes up comes back down as the same files.
func TestN64RoundTrip(t *testing.T) {
	pak := formattedPak()
	pak[n64PakIndex+1] = 0x05 // a note's first page
	pak[0x500] = 0x42
	paks := append(bytes.Repeat(formattedPak(), 1), pak...)
	paks = append(paks, bytes.Repeat(formattedPak(), 2)...)
	s := &N64Save{}
	s.TakeFile(".eep", n64File(0x200, 0, 9))
	s.TakeFile(".fla", n64File(0x20000, 4, 1, 2, 3, 4))
	s.TakeFile(".mpk", paks)

	back, err := ParseN64Unit(s.Unit())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range back.Files(nil, nil) {
		files[f.Ext] = f.Data
	}
	if files[".sra"] != nil || len(files[".eep"]) != 0x800 || files[".eep"][0] != 9 {
		t.Fatalf("eep %d, sra %v", len(files[".eep"]), files[".sra"] != nil)
	}
	if !bytes.Equal(files[".fla"][4:8], []byte{1, 2, 3, 4}) {
		t.Fatalf("flash %x", files[".fla"][4:8])
	}
	if !bytes.Equal(files[".mpk"][n64PakSize:2*n64PakSize], pak) || !bytes.Equal(files[".mpk"][:n64PakSize], formattedPak()) {
		t.Fatal("paks not restored")
	}
}

// A libretro core's .srm, as RetroArch saves an N64 game, restores into mupen64plus's files.
func TestN64FromLibretroSRM(t *testing.T) {
	blob := bytes.Repeat([]byte{0xFF}, n64BlobSize)
	copy(blob[n64BlobSRAM:], []byte{0xA, 0xB, 0xC, 0xD})
	s, err := ParseN64Unit(blob)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Files(nil, nil) {
		if f.Ext == ".sra" && !bytes.Equal(f.Data[:4], []byte{0xA, 0xB, 0xC, 0xD}) {
			t.Fatalf("sra %x", f.Data[:4])
		}
		if f.Ext == ".eep" && f.Data != nil {
			t.Fatal("blank EEPROM restored")
		}
	}
}

func TestN64UnitRefusesOtherSaves(t *testing.T) {
	if _, err := ParseN64Unit([]byte("a memory card, 131072 bytes in a real one")); err == nil {
		t.Fatal("accepted")
	}
}

// RomM keeps a zip's hash over its members, so the zip's own bytes don't matter.
func TestContentHash(t *testing.T) {
	zipOf := func(names ...string) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, n := range names {
			w, _ := zw.Create(n)
			_, _ = w.Write([]byte(n + " bytes"))
		}
		_ = zw.Close()
		return buf.Bytes()
	}
	a, b := zipOf("sram", "eeprom"), zipOf("eeprom", "sram")
	if bytes.Equal(a, b) || ContentHash(a) != ContentHash(b) {
		t.Fatal("member order changed the hash")
	}
	want := md5Hex([]byte("eeprom:" + md5Hex([]byte("eeprom bytes")) + "\nsram:" + md5Hex([]byte("sram bytes"))))
	if ContentHash(a) != want {
		t.Fatalf("hash %s, want %s", ContentHash(a), want)
	}
	if ContentHash([]byte("raw")) != md5Hex([]byte("raw")) {
		t.Fatal("raw hash")
	}
}

func TestN64SaveName(t *testing.T) {
	rom := func(order string) []byte {
		z := make([]byte, 0x1000)
		copy(z, []byte{0x80, 0x37, 0x12, 0x40})
		// The header's name field is 20 bytes; mupen64plus trims its spaces.
		copy(z[0x20:], "  ZELDA MAJORAS MASK")
		switch order {
		case "v64":
			for i := 0; i < len(z); i += 2 {
				z[i], z[i+1] = z[i+1], z[i]
			}
		case "n64":
			swap32(z)
		}
		return z
	}
	z, _ := ParseN64ROM(rom("z64"))
	for _, order := range []string{"v64", "n64"} {
		if got, err := ParseN64ROM(rom(order)); err != nil || got.MD5 != z.MD5 {
			t.Fatalf("%s: %+v, %v", order, got, err)
		}
	}
	if string(z.Header) != "ZELDA MAJORAS MASK" {
		t.Fatalf("header %q", z.Header)
	}
	cases := map[string]struct {
		good  string
		known bool
		want  string
	}{
		"in the database":  {"Legend of Zelda, The - Ocarina of Time (U) (V1.0) [!]", true, "Legend of Zelda, The - Ocarina o-" + z.MD5[:8]},
		"a sanitised name": {"F-Zero X: Expansion", true, "F-Zero X_ Expansion-" + z.MD5[:8]},
		"not in it":        {"", false, "ZELDA MAJORAS MASK-" + z.MD5[:8]},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := N64SaveName(z, c.good, c.known); got != c.want {
				t.Fatalf("got %q", got)
			}
		})
	}
	if got := N64SaveName(N64ROM{MD5: z.MD5}, "", false); got != "unknown-"+z.MD5[:8] {
		t.Fatalf("no header: %q", got)
	}
}

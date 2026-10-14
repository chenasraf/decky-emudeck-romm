package emudeck

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const n64ini = `; mupen64plus ROM database
[AAAA0000AAAA0000AAAA0000AAAA0000]
GoodName=Legend of Zelda, The - Ocarina of Time (U) (V1.0) [!]
CRC=EC7011B7 7616D72B
SaveType=SRAM

[BBBB0000BBBB0000BBBB0000BBBB0000]
RefMD5=AAAA0000AAAA0000AAAA0000AAAA0000

[CCCC0000CCCC0000CCCC0000CCCC0000]
GoodName=Twin A
CRC=11111111 22222222

[DDDD0000DDDD0000DDDD0000DDDD0000]
GoodName=Twin B
CRC=11111111 22222222
`

func writeDB(t *testing.T, home, content string) string {
	t.Helper()
	p := filepath.Join(home, rmgDatabase[0])
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestN64Database(t *testing.T) {
	home := t.TempDir()
	writeDB(t, home, n64ini)
	db := NewN64Database(home)

	cases := map[string]struct {
		rom   domain.N64ROM
		name  string
		known bool
	}{
		"by MD5":              {domain.N64ROM{MD5: "AAAA0000AAAA0000AAAA0000AAAA0000"}, "Legend of Zelda, The - Ocarina of Time (U) (V1.0) [!]", true},
		"through its RefMD5":  {domain.N64ROM{MD5: "BBBB0000BBBB0000BBBB0000BBBB0000"}, "Legend of Zelda, The - Ocarina of Time (U) (V1.0) [!]", true},
		"by CRC":              {domain.N64ROM{MD5: "FFFF", CRC1: 0xEC7011B7, CRC2: 0x7616D72B}, "Legend of Zelda, The - Ocarina of Time (U) (V1.0) [!]", true},
		"by a CRC two share":  {domain.N64ROM{MD5: "FFFF", CRC1: 0x11111111, CRC2: 0x22222222}, "", false},
		"not in the database": {domain.N64ROM{MD5: "FFFF", CRC1: 1, CRC2: 2}, "", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, known, err := db.GoodName(c.rom)
			if err != nil || got != c.name || known != c.known {
				t.Fatalf("got %q, %v, %v", got, known, err)
			}
		})
	}
}

// An RMG update that changes the database is read again.
func TestN64DatabaseReloads(t *testing.T) {
	home := t.TempDir()
	p := writeDB(t, home, n64ini)
	db := NewN64Database(home)
	rom := domain.N64ROM{MD5: "EEEE0000EEEE0000EEEE0000EEEE0000"}
	if _, known, _ := db.GoodName(rom); known {
		t.Fatal("known before")
	}
	writeDB(t, home, n64ini+"\n[EEEE0000EEEE0000EEEE0000EEEE0000]\nGoodName=A Hack\n")
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(p, later, later)
	if name, known, _ := db.GoodName(rom); !known || name != "A Hack" {
		t.Fatalf("after: %q, %v", name, known)
	}
}

func TestN64DatabaseMissing(t *testing.T) {
	if _, _, err := NewN64Database(t.TempDir()).GoodName(domain.N64ROM{MD5: "A"}); err == nil {
		t.Fatal("no error without RMG")
	}
}

func TestRMGSaveFilenameFormat(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".var", "app", "com.github.Rosalie241.RMG", "config", "RMG", "mupen64plus.cfg")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	c := NewSaveConfigs(home)
	for format, problem := range map[string]bool{"1": false, "0": true} {
		if err := os.WriteFile(cfg, []byte("[Rosalie's Mupen GUI Core Overlay]\nSaveFilenameFormat = "+format+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := c.SaveConfig("rmg").Problem; (got != "") != problem {
			t.Fatalf("format %s: %q", format, got)
		}
	}
}

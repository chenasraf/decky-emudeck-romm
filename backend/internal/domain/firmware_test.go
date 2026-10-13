package domain

import "testing"

func TestFirmwareStatusOf(t *testing.T) {
	sums := FileSums{CRC: "0a1b2c3d", MD5: "md5-on-disk", SHA1: "sha1-on-disk"}
	for _, c := range []struct {
		name string
		f    Firmware
		size int64
		want BIOSStatus
	}{
		{"sha1 matches, any case", Firmware{SHA1: "SHA1-ON-DISK", MD5: "other"}, 1, BIOSPresent},
		{"sha1 wins over a matching md5", Firmware{SHA1: "other", MD5: "md5-on-disk"}, 1, BIOSDifferent},
		{"md5 without sha1", Firmware{MD5: "md5-on-disk"}, 1, BIOSPresent},
		{"crc ignores leading zeros", Firmware{CRC: "a1b2c3d"}, 1, BIOSPresent},
		{"crc differs", Firmware{CRC: "ffffffff"}, 1, BIOSDifferent},
		{"no hash, same size", Firmware{SizeBytes: 512}, 512, BIOSPresent},
		{"no hash, other size", Firmware{SizeBytes: 512}, 511, BIOSDifferent},
	} {
		if got := c.f.StatusOf(c.size, sums); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestFirmwareSafeName(t *testing.T) {
	for name, ok := range map[string]bool{
		"scph5501.bin": true,
		"":             false,
		"..":           false,
		"../evil.bin":  false,
		`dc\boot.bin`:  false,
		"dc/boot.bin":  false,
	} {
		if _, got := (Firmware{FileName: name}).SafeName(); got != ok {
			t.Errorf("%q: %v, want %v", name, got, ok)
		}
	}
}

func TestBIOSFolder(t *testing.T) {
	for slug, want := range map[string]string{"psx": "", "dc": "dc", "neo-geo-cd": "neocd"} {
		if got, ok := BIOSFolder(slug); !ok || got != want {
			t.Errorf("%s: %q, %v", slug, got, ok)
		}
	}
	if _, ok := BIOSFolder("switch"); ok {
		t.Error("switch firmware placed in bios/")
	}
}

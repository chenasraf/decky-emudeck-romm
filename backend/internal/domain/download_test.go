package domain

import "testing"

func TestFormatSize(t *testing.T) {
	for bytes, want := range map[int64]string{
		0:                       "0 B",
		512:                     "512 B",
		1536:                    "1.5 KB",
		300 << 20:               "300 MB",
		4*(1<<30) + 200*(1<<20): "4.2 GB",
		12 << 30:                "12 GB",
	} {
		if got := FormatSize(bytes); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", bytes, got, want)
		}
	}
}

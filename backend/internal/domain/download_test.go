package domain

import (
	"testing"
	"time"
)

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

func TestUpdateAvailable(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	rom := ROMFile{ID: 1, SizeBytes: 10, Category: "game", LastModified: at, SHA1: "ABC"}
	manual := ROMFile{ID: 2, SizeBytes: 5, Category: "manual", LastModified: at}
	snapshot := []FileSnapshot{SnapshotOf(rom)}

	cases := []struct {
		name  string
		files []ROMFile
		want  bool
	}{
		{"unchanged", []ROMFile{rom, manual}, false},
		{"hash case differs", []ROMFile{func() ROMFile { f := rom; f.SHA1 = "abc"; return f }()}, false},
		{"touched but same hash", []ROMFile{func() ROMFile { f := rom; f.LastModified = at.Add(time.Hour); return f }()}, false},
		{"re-hashed", []ROMFile{func() ROMFile { f := rom; f.SHA1 = "def"; return f }()}, true},
		{"replaced", []ROMFile{func() ROMFile { f := rom; f.ID = 3; return f }()}, true},
		{"file added", []ROMFile{rom, {ID: 4, Category: "game"}}, true},
		{"file removed", nil, true},
		{"new manual only", []ROMFile{rom, manual, {ID: 5, Category: "manual"}}, false},
	}
	for _, c := range cases {
		if got := UpdateAvailable(snapshot, c.files); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}

	// Without hashes, size and modification time decide.
	bare := ROMFile{ID: 1, SizeBytes: 10, Category: "game", LastModified: at}
	unhashed := []FileSnapshot{SnapshotOf(bare)}
	if UpdateAvailable(unhashed, []ROMFile{bare}) {
		t.Error("unchanged unhashed file")
	}
	bare.LastModified = at.Add(time.Second)
	if !UpdateAvailable(unhashed, []ROMFile{bare}) {
		t.Error("touched unhashed file")
	}
}

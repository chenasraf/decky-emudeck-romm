package romm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestFirmware(t *testing.T) {
	bios := []byte("psx bios")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/firmware" && r.URL.Query().Get("platform_id") == "3":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 31, "platform_id": 3, "file_name": "scph5501.bin", "file_size_bytes": 524288, "is_verified": true,
					"crc_hash": "8d8cb7e4", "md5_hash": "490f666e", "sha1_hash": nil, "missing_from_fs": false},
				{"id": 32, "platform_id": 3, "file_name": "gone.bin", "missing_from_fs": true},
			})
		case r.URL.Path == "/api/firmware/31/content/scph5501.bin":
			_, _ = w.Write(bios)
		case r.URL.Path == "/api/firmware":
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "rmm_x", ver)
	ctx := context.Background()

	got, err := c.Firmware(ctx, 3)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	want := domain.Firmware{ID: 31, PlatformID: 3, FileName: "scph5501.bin", SizeBytes: 524288, CRC: "8d8cb7e4", MD5: "490f666e", Verified: true}
	if got[0] != want {
		t.Fatalf("firmware %+v", got[0])
	}
	if _, err := c.Firmware(ctx, 4); kindOf(err) != domain.KindMissingPermission {
		t.Fatalf("without firmware.read: %v", err)
	}

	var buf bytes.Buffer
	if err := c.DownloadFirmware(ctx, 31, "scph5501.bin", func(int64) (io.Writer, error) { return &buf, nil }); err != nil || !bytes.Equal(buf.Bytes(), bios) {
		t.Fatalf("content %q, %v", buf.Bytes(), err)
	}
}

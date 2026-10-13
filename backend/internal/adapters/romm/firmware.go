package romm

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// Firmware returns the BIOS and firmware files the server has for a platform.
func (c *Client) Firmware(ctx context.Context, platformID int) ([]domain.Firmware, error) {
	var body []struct {
		ID            int     `json:"id"`
		PlatformID    int     `json:"platform_id"`
		FileName      string  `json:"file_name"`
		FileSizeBytes int64   `json:"file_size_bytes"`
		IsVerified    bool    `json:"is_verified"`
		CRCHash       *string `json:"crc_hash"`
		MD5Hash       *string `json:"md5_hash"`
		SHA1Hash      *string `json:"sha1_hash"`
		// MissingFromFS is set when the server lists a file it no longer has on disk.
		MissingFromFS bool `json:"missing_from_fs"`
	}
	status, err := c.getBulk(ctx, "/api/firmware?platform_id="+strconv.Itoa(platformID), &body)
	if err := statusError("list BIOS files", status, err); err != nil {
		return nil, err
	}
	out := make([]domain.Firmware, 0, len(body))
	for _, f := range body {
		if f.MissingFromFS {
			continue
		}
		out = append(out, domain.Firmware{
			ID:         f.ID,
			PlatformID: f.PlatformID,
			FileName:   f.FileName,
			SizeBytes:  f.FileSizeBytes,
			CRC:        deref(f.CRCHash),
			MD5:        deref(f.MD5Hash),
			SHA1:       deref(f.SHA1Hash),
			Verified:   f.IsVerified,
		})
	}
	return out, nil
}

// DownloadFirmware fetches the content of the firmware file with id; see DownloadFile.
func (c *Client) DownloadFirmware(ctx context.Context, id int, name string, open func(start int64) (io.Writer, error)) error {
	return c.fetchContent(ctx, fmt.Sprintf("/api/firmware/%d/content/%s", id, url.PathEscape(name)), 0, open)
}

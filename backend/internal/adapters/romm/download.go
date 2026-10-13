package romm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// stallTimeout is how long a download may go without receiving a byte before it counts as
// dropped. Downloads have no overall timeout: a large game on a slow link takes as long as it
// takes.
const stallTimeout = 60 * time.Second

// ROMFiles returns the files of the ROM with id.
func (c *Client) ROMFiles(ctx context.Context, id int) ([]domain.ROMFile, error) {
	var body struct {
		Files []struct {
			ID            int     `json:"id"`
			FileName      string  `json:"file_name"`
			FileSizeBytes int64   `json:"file_size_bytes"`
			Category      *string `json:"category"`
			LastModified  string  `json:"last_modified"`
			CRCHash       *string `json:"crc_hash"`
			MD5Hash       *string `json:"md5_hash"`
			SHA1Hash      *string `json:"sha1_hash"`
		} `json:"files"`
	}
	// A ROM's details carry its full metadata, which can outgrow maxBody.
	status, err := c.exchange(ctx, c.http, bulkMaxBody, http.MethodGet, "/api/roms/"+strconv.Itoa(id), nil, &body)
	if status == http.StatusNotFound {
		return nil, &domain.Error{Kind: domain.KindNotFound, Message: "That game is no longer on RomM. Refresh the library."}
	}
	if err := statusError("read the game", status, err); err != nil {
		return nil, err
	}
	out := make([]domain.ROMFile, 0, len(body.Files))
	for _, f := range body.Files {
		modified, _ := time.Parse(time.RFC3339Nano, f.LastModified)
		out = append(out, domain.ROMFile{
			ID:           f.ID,
			Name:         f.FileName,
			SizeBytes:    f.FileSizeBytes,
			Category:     deref(f.Category),
			LastModified: modified.UTC(),
			CRC:          deref(f.CRCHash),
			MD5:          deref(f.MD5Hash),
			SHA1:         deref(f.SHA1Hash),
		})
	}
	return out, nil
}

// DownloadFile writes the content of the ROM file with id to w.
func (c *Client) DownloadFile(ctx context.Context, id int, name string, w io.Writer) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	// The path's last segment only names the download; RomM serves the file by id. Escaping it
	// fully keeps names with #, ? or % from cutting the URL short.
	path := fmt.Sprintf("/api/roms/%d/files/content/%s", id, url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Authorization", "Bearer "+c.token)

	start := time.Now()
	watchdog := time.AfterFunc(stallTimeout, func() { cancel(errStalled) })
	defer watchdog.Stop()
	resp, err := c.download.Do(req)
	if err != nil {
		c.log.Warn("download failed", "url", c.base+path, "err", err)
		return c.downloadError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError("download the game", resp.StatusCode, nil); err != nil {
		return err
	}

	n, err := io.Copy(writer{w}, &resetReader{r: resp.Body, timer: watchdog})
	c.log.Debug("downloaded", "url", c.base+path, "bytes", n, "duration", time.Since(start).Round(time.Millisecond))
	if err != nil {
		var werr writeError
		if errors.As(err, &werr) {
			return werr.err
		}
		c.log.Warn("download cut off", "url", c.base+path, "bytes", n, "err", err)
		return c.downloadError(ctx, err)
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return unreachable(fmt.Errorf("got %d of %d bytes", n, resp.ContentLength))
	}
	return nil
}

var errStalled = errors.New("no data for 60 s")

// downloadError says why a download stopped: the caller cancelled it, or the network dropped or
// stalled it.
func (c *Client) downloadError(ctx context.Context, err error) error {
	cause := context.Cause(ctx)
	switch {
	case errors.Is(cause, errStalled):
		return unreachable(cause)
	case cause != nil:
		return cause
	default:
		return unreachable(err)
	}
}

func unreachable(err error) error {
	return &domain.Error{Kind: domain.KindUnreachable, Message: "The download from RomM was interrupted.", Err: err}
}

// writer tags the errors of the file being written, so a full disk isn't mistaken for a dropped
// connection.
type writer struct{ w io.Writer }

type writeError struct{ err error }

func (e writeError) Error() string { return e.err.Error() }

func (w writer) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if err != nil {
		return n, writeError{err}
	}
	return n, nil
}

// resetReader pushes timer back on every read that receives data.
type resetReader struct {
	r     io.Reader
	timer *time.Timer
}

func (r *resetReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.timer.Reset(stallTimeout)
	}
	return n, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

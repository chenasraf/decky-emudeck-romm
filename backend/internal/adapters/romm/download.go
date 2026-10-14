package romm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// stallTimeout is how long a download may go without receiving a byte before it counts as
// dropped. Downloads have no overall timeout: a large game on a slow link takes as long as it
// takes.
const stallTimeout = 60 * time.Second

// romWithFiles is a ROM and its files, as RomM describes them.
type romWithFiles struct {
	ID     int        `json:"id"`
	FSPath string     `json:"fs_path"`
	FSName string     `json:"fs_name"`
	Files  []wireFile `json:"files"`
}

type wireFile struct {
	ID            int     `json:"id"`
	FileName      string  `json:"file_name"`
	FilePath      string  `json:"file_path"`
	FullPath      string  `json:"full_path"`
	FileSizeBytes int64   `json:"file_size_bytes"`
	Category      *string `json:"category"`
	LastModified  string  `json:"last_modified"`
	CRCHash       *string `json:"crc_hash"`
	MD5Hash       *string `json:"md5_hash"`
	SHA1Hash      *string `json:"sha1_hash"`
}

func (r romWithFiles) files() []domain.ROMFile {
	out := make([]domain.ROMFile, 0, len(r.Files))
	for _, f := range r.Files {
		modified, _ := time.Parse(time.RFC3339Nano, f.LastModified)
		out = append(out, domain.ROMFile{
			ID:           f.ID,
			Name:         f.FileName,
			Path:         pathInGame(r.FSPath, r.FSName, f.FilePath, f.FileName, f.FullPath),
			SizeBytes:    f.FileSizeBytes,
			Category:     deref(f.Category),
			LastModified: modified.UTC(),
			CRC:          deref(f.CRCHash),
			MD5:          deref(f.MD5Hash),
			SHA1:         deref(f.SHA1Hash),
		})
	}
	return out
}

// ROMFiles returns the files of the ROM with id.
func (c *Client) ROMFiles(ctx context.Context, id int) ([]domain.ROMFile, error) {
	var body romWithFiles
	// A ROM's details carry its full metadata, which can outgrow maxBody.
	status, err := c.exchange(ctx, c.http, bulkMaxBody, http.MethodGet, "/api/roms/"+strconv.Itoa(id), nil, &body)
	if status == http.StatusNotFound {
		return nil, &domain.Error{Kind: domain.KindNotFound, Message: "That game is no longer on RomM. Refresh the library."}
	}
	if err := statusError("read the game", status, err); err != nil {
		return nil, err
	}
	return body.files(), nil
}

// PlatformFiles returns the files of up to limit of a platform's ROMs from offset, ordered by id.
func (c *Client) PlatformFiles(ctx context.Context, platformID, offset, limit int) ([]domain.ROMWithFiles, error) {
	q := url.Values{
		"platform_ids":      {strconv.Itoa(platformID)},
		"limit":             {strconv.Itoa(limit)},
		"offset":            {strconv.Itoa(offset)},
		"order_by":          {"id"},
		"order_dir":         {"asc"},
		"with_files":        {"true"},
		"with_rom_id_index": {"false"},
	}
	var body struct {
		Items []romWithFiles `json:"items"`
	}
	status, err := c.getBulk(ctx, "/api/roms?"+romQuery+"&"+q.Encode(), &body)
	if err := statusError("list the platform's files", status, err); err != nil {
		return nil, err
	}
	out := make([]domain.ROMWithFiles, 0, len(body.Items))
	for _, r := range body.Items {
		out = append(out, domain.ROMWithFiles{ROMID: r.ID, Files: r.files()})
	}
	return out, nil
}

// pathInGame is where a file sits inside its game, with / separators. However the Server's library
// is laid out, a game is the file or folder fs_name in fs_path and its files are under it, so the
// path inside the game is the file's full path past that prefix; RomM names files in its own
// downloads the same way. A file outside it, which RomM doesn't produce, falls back to its name.
func pathInGame(fsPath, fsName, filePath, fileName, fullPath string) string {
	if fullPath == "" {
		fullPath = path.Join(filePath, fileName)
	}
	game := path.Join(fsPath, fsName)
	if rel, ok := strings.CutPrefix(path.Clean(fullPath), game+"/"); ok && rel != "" {
		return rel
	}
	return fileName
}

// DownloadFile fetches the content of the ROM file with id from byte offset from onward. Once the
// server answers, it calls open with the offset its bytes start at: from when the server resumes,
// or zero when it sends the whole file. It writes the bytes to the writer open returns.
func (c *Client) DownloadFile(ctx context.Context, id int, name string, from int64, open func(start int64) (io.Writer, error)) error {
	// The path's last segment only names the download; RomM serves the file by id. Escaping it
	// fully keeps names with #, ? or % from cutting the URL short.
	return c.fetchContent(ctx, fmt.Sprintf("/api/roms/%d/files/content/%s", id, url.PathEscape(name)), from, open)
}

// convertedPath is the URL of a ROM's file served in format: the ROM-level content endpoint, the
// one RomM converts on, narrowed to the one file.
func convertedPath(romID, fileID int, name, format string) string {
	q := url.Values{"file_ids": {strconv.Itoa(fileID)}, "format": {format}}
	return fmt.Sprintf("/api/roms/%d/content/%s?%s", romID, url.PathEscape(name), q.Encode())
}

// convertPoll caps how long Convert waits between asks while the server converts, whatever its
// Retry-After says.
const convertPoll = 30 * time.Second

// Convert asks the server for a ROM's file in format and returns the file it will serve, waiting
// while it converts. A server that predates conversion serves the file as stored, so the name it
// returns may keep the stored format. It fails with domain.ErrNoConversion when the server won't
// convert the file.
func (c *Client) Convert(ctx context.Context, romID, fileID int, name, format string) (domain.ServedFile, error) {
	for {
		served, wait, err := c.askConverted(ctx, romID, fileID, name, format)
		if err != nil || wait == 0 {
			return served, err
		}
		select {
		case <-ctx.Done():
			return served, ctx.Err()
		case <-time.After(min(wait, convertPoll)):
		}
	}
}

// askConverted asks for the first byte of the converted file, which starts a conversion that
// isn't cached and tells the file's name and size without downloading it. wait is how long to
// wait before asking again while it converts.
func (c *Client) askConverted(ctx context.Context, romID, fileID int, name, format string) (domain.ServedFile, time.Duration, error) {
	path := convertedPath(romID, fileID, name, format)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return domain.ServedFile{}, 0, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.http.Do(req)
	if err != nil {
		return domain.ServedFile{}, 0, unreachable(err)
	}
	// Only the headers matter; a server that ignores Range would send the whole file.
	_ = resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusAccepted:
		wait := 5 * time.Second
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			wait = time.Duration(secs) * time.Second
		}
		c.log.Debug("converting", "rom_id", romID, "format", format, "retry_after", wait)
		return domain.ServedFile{}, wait, nil
	case resp.StatusCode == http.StatusNotAcceptable:
		return domain.ServedFile{}, 0, domain.ErrNoConversion
	case resp.StatusCode == http.StatusNotFound:
		return domain.ServedFile{}, 0, &domain.Error{Kind: domain.KindNotFound, Message: "That game is no longer on RomM. Refresh the library."}
	case resp.StatusCode >= http.StatusInternalServerError:
		return domain.ServedFile{}, 0, unreachable(fmt.Errorf("server answered %d", resp.StatusCode))
	case resp.StatusCode != http.StatusPartialContent:
		if err := statusError("download the game", resp.StatusCode, nil); err != nil {
			return domain.ServedFile{}, 0, err
		}
	}
	served := domain.ServedFile{Name: dispositionName(resp.Header.Get("Content-Disposition"), name), Size: resp.ContentLength}
	if resp.StatusCode == http.StatusPartialContent {
		served.Size = contentRangeSize(resp.Header.Get("Content-Range"))
	}
	return served, 0, nil
}

// DownloadConverted fetches a ROM's file in format, as Convert found it ready, from byte offset
// from onward; see DownloadFile. A server that dropped the converted copy since is converting it
// again, which counts as a dropped download so the caller retries.
func (c *Client) DownloadConverted(ctx context.Context, romID, fileID int, name, format string, from int64, open func(start int64) (io.Writer, error)) error {
	return c.fetchContent(ctx, convertedPath(romID, fileID, name, format), from, open)
}

func (c *Client) fetchContent(ctx context.Context, path string, from int64, open func(start int64) (io.Writer, error)) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if from > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", from))
	}

	start := time.Now()
	watchdog := time.AfterFunc(stallTimeout, func() { cancel(errStalled) })
	defer watchdog.Stop()
	resp, err := c.download.Do(req)
	if err != nil {
		c.log.Warn("download failed", "url", c.base+path, "err", err)
		return c.downloadError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()

	offset := int64(0)
	switch {
	case resp.StatusCode == http.StatusPartialContent && from > 0:
		if got := contentRangeStart(resp.Header.Get("Content-Range")); got != from {
			return unreachable(fmt.Errorf("asked for bytes from %d, got a range from %d", from, got))
		}
		offset = from
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && from > 0:
		// The file on the server is shorter than what was kept, so the kept bytes aren't of it.
		_ = resp.Body.Close()
		return c.fetchContent(ctx, path, 0, open)
	case resp.StatusCode == http.StatusAccepted:
		return unreachable(errors.New("the server is converting the file again"))
	case resp.StatusCode >= http.StatusInternalServerError:
		// A proxy in front of a restarting server answers 502 to 504; the download is worth retrying.
		return unreachable(fmt.Errorf("server answered %d", resp.StatusCode))
	default:
		if err := statusError("download the game", resp.StatusCode, nil); err != nil {
			return err
		}
	}
	w, err := open(offset)
	if err != nil {
		return err
	}

	n, err := io.Copy(writer{w}, &resetReader{r: resp.Body, timer: watchdog})
	c.log.Debug("downloaded", "url", c.base+path, "bytes", n, "duration", time.Since(start).Round(time.Millisecond))
	if err != nil {
		if werr, ok := errors.AsType[writeError](err); ok {
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

// dispositionName is the file name a Content-Disposition gives, or fallback when it gives none.
// Only the last path element counts: the name is used for a file on disk.
func dispositionName(header, fallback string) string {
	_, params, err := mime.ParseMediaType(header)
	if err != nil || params["filename"] == "" {
		return fallback
	}
	name := path.Base(strings.ReplaceAll(params["filename"], `\`, "/"))
	if name == "." || name == "/" || name == ".." {
		return fallback
	}
	return name
}

// contentRangeSize is the total size a "bytes first-last/size" Content-Range gives, or -1.
func contentRangeSize(header string) int64 {
	_, total, ok := strings.Cut(header, "/")
	if !ok {
		return -1
	}
	n, err := strconv.ParseInt(total, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// contentRangeStart returns the first byte offset of a "bytes first-last/size" Content-Range, or
// -1 when it isn't one.
func contentRangeStart(header string) int64 {
	spec, ok := strings.CutPrefix(header, "bytes ")
	if !ok {
		return -1
	}
	first, _, ok := strings.Cut(spec, "-")
	if !ok {
		return -1
	}
	n, err := strconv.ParseInt(first, 10, 64)
	if err != nil {
		return -1
	}
	return n
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

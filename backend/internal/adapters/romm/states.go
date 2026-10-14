package romm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const (
	// maxState bounds a save state download; GBA states are about half a megabyte, the largest
	// consoles' tens of megabytes.
	maxState = 256 << 20
	// maxScreenshot bounds a state's screenshot download.
	maxScreenshot = 8 << 20
)

type wireState struct {
	ID         int     `json:"id"`
	ROMID      int     `json:"rom_id"`
	FileName   string  `json:"file_name"`
	Emulator   *string `json:"emulator"`
	SizeBytes  int64   `json:"file_size_bytes"`
	CreatedAt  string  `json:"created_at"`
	Screenshot *struct {
		DownloadPath string `json:"download_path"`
	} `json:"screenshot"`
}

func (s wireState) state() domain.ServerState {
	out := domain.ServerState{
		ID: s.ID, ROMID: s.ROMID, FileName: s.FileName, Emulator: deref(s.Emulator), SizeBytes: s.SizeBytes,
		CreatedAt: parseTime(s.CreatedAt),
	}
	if s.Screenshot != nil {
		out.Screenshot = s.Screenshot.DownloadPath
	}
	return out
}

// States lists the save states RomM keeps of the ROM with id, or of every ROM when id is 0.
func (c *Client) States(ctx context.Context, id int) ([]domain.ServerState, error) {
	path := "/api/states"
	if id != 0 {
		path += "?rom_id=" + strconv.Itoa(id)
	}
	var body []wireState
	status, err := c.exchange(ctx, c.bulk, bulkMaxBody, http.MethodGet, path, nil, &body)
	if err := statusError("list the save states", status, err); err != nil {
		return nil, err
	}
	out := make([]domain.ServerState, 0, len(body))
	for _, s := range body {
		out = append(out, s.state())
	}
	return out, nil
}

// UploadState adds a save state, and its screenshot when it has one, to its game's list.
func (c *Client) UploadState(ctx context.Context, u domain.StateUpload) (domain.ServerState, error) {
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	part, err := mw.CreateFormFile("stateFile", u.FileName)
	if err != nil {
		return domain.ServerState{}, err
	}
	if _, err := part.Write(u.Data); err != nil {
		return domain.ServerState{}, err
	}
	if len(u.Screenshot) > 0 {
		// RomM pairs a state with the screenshot of the same name.
		shot := strings.TrimSuffix(u.FileName, ".state") + ".png"
		part, err := mw.CreateFormFile("screenshotFile", shot)
		if err != nil {
			return domain.ServerState{}, err
		}
		if _, err := part.Write(u.Screenshot); err != nil {
			return domain.ServerState{}, err
		}
	}
	if err := mw.Close(); err != nil {
		return domain.ServerState{}, err
	}
	q := url.Values{"rom_id": {strconv.Itoa(u.ROMID)}}
	if u.Emulator != "" {
		q.Set("emulator", u.Emulator)
	}
	var body wireState
	status, err := c.send(ctx, http.MethodPost, "/api/states?"+q.Encode(), mw.FormDataContentType(), &form, &body)
	if err := statusError("upload the save state", status, err); err != nil {
		return domain.ServerState{}, err
	}
	return body.state(), nil
}

// DownloadState fetches a save state's content.
func (c *Client) DownloadState(ctx context.Context, id int) ([]byte, error) {
	return c.fetchBytes(ctx, "/api/states/"+strconv.Itoa(id)+"/content", maxState, "the save state")
}

// StateScreenshot fetches a state's screenshot from the path RomM listed it at.
func (c *Client) StateScreenshot(ctx context.Context, path string) ([]byte, error) {
	if !strings.HasPrefix(path, "/api/") {
		return nil, fmt.Errorf("screenshot path %q isn't RomM's", path)
	}
	// RomM lists the path with its query unencoded, a timestamp's space and all.
	u, err := url.Parse(strings.ReplaceAll(path, " ", "%20"))
	if err != nil {
		return nil, fmt.Errorf("screenshot path %q: %w", path, err)
	}
	u.RawQuery = u.Query().Encode()
	return c.fetchBytes(ctx, u.String(), maxScreenshot, "the screenshot")
}

// DeleteStates deletes save states from RomM.
func (c *Client) DeleteStates(ctx context.Context, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	status, err := c.do(ctx, http.MethodPost, "/api/states/delete", map[string][]int{"states": ids}, nil)
	if err == nil && status == http.StatusNotFound {
		// One of them went already; RomM stops at it, so the rest go one by one.
		if len(ids) == 1 {
			return nil
		}
		for _, id := range ids {
			if err := c.DeleteStates(ctx, []int{id}); err != nil {
				return err
			}
		}
		return nil
	}
	return statusError("delete save states", status, err)
}

// fetchBytes downloads a file RomM serves at path, of at most limit bytes.
func (c *Client) fetchBytes(ctx context.Context, path string, limit int64, what string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		c.log.Warn("download failed", "what", what, "err", err)
		return nil, noAnswer(err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, &domain.Error{Kind: domain.KindNotFound, Message: "RomM no longer has " + what + "."}
	default:
		if err := statusError("download "+what, resp.StatusCode, nil); err != nil {
			return nil, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, &domain.Error{Kind: domain.KindUnreachable, Message: "Couldn't reach the server.", Err: err}
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is over %d bytes", what, limit)
	}
	return data, nil
}

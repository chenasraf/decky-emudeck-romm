package romm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const (
	// saveVersions is how many versions of a slot RomM keeps once the plugin uploads to it.
	saveVersions = 10
	// maxSave bounds a save download; RetroArch's battery saves are a few hundred KB at most.
	maxSave = 64 << 20
)

type wireSave struct {
	ID          int     `json:"id"`
	ROMID       int     `json:"rom_id"`
	FileName    string  `json:"file_name"`
	Slot        *string `json:"slot"`
	Emulator    *string `json:"emulator"`
	ContentHash *string `json:"content_hash"`
	UpdatedAt   string  `json:"updated_at"`
}

func (s wireSave) save() domain.ServerSave {
	return domain.ServerSave{
		ID: s.ID, ROMID: s.ROMID, FileName: s.FileName, Slot: deref(s.Slot), Emulator: deref(s.Emulator),
		ContentHash: deref(s.ContentHash), UpdatedAt: parseTime(s.UpdatedAt),
	}
}

// parseTime reads RomM's timestamps, which carry an offset or a Z; one that doesn't parse is zero.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// errSyncDisabled is RomM's answer to a sync of a Device whose sync is off.
var errSyncDisabled = &domain.Error{
	Kind:    domain.KindSyncDisabled,
	Message: "RomM has save sync turned off for this Deck. Turn it on in RomM's device settings.",
}

// NegotiateSync asks RomM what a sync of the Deck's saves takes.
func (c *Client) NegotiateSync(ctx context.Context, req domain.SyncRequest) (domain.SyncPlan, error) {
	type clientSave struct {
		ROMID       int    `json:"rom_id"`
		FileName    string `json:"file_name"`
		Slot        string `json:"slot"`
		Emulator    string `json:"emulator,omitempty"`
		ContentHash string `json:"content_hash,omitempty"`
		UpdatedAt   string `json:"updated_at"`
		SizeBytes   int64  `json:"file_size_bytes"`
	}
	in := struct {
		DeviceID        string       `json:"device_id"`
		Saves           []clientSave `json:"saves"`
		ROMIDs          []int        `json:"rom_ids"`
		RestoreUnlisted bool         `json:"restore_unlisted"`
	}{DeviceID: req.DeviceID, Saves: []clientSave{}, ROMIDs: req.ROMIDs, RestoreUnlisted: req.RestoreUnlisted}
	for _, s := range req.Saves {
		in.Saves = append(in.Saves, clientSave{
			ROMID: s.ROMID, FileName: s.FileName, Slot: s.Slot, Emulator: s.Emulator, ContentHash: s.ContentHash,
			UpdatedAt: s.UpdatedAt.UTC().Format(time.RFC3339Nano), SizeBytes: s.SizeBytes,
		})
	}
	var body struct {
		SessionID  int `json:"session_id"`
		Operations []struct {
			Action          string  `json:"action"`
			ROMID           int     `json:"rom_id"`
			SaveID          *int    `json:"save_id"`
			FileName        string  `json:"file_name"`
			Slot            *string `json:"slot"`
			Emulator        *string `json:"emulator"`
			Reason          string  `json:"reason"`
			ServerUpdatedAt *string `json:"server_updated_at"`
			ServerHash      *string `json:"server_content_hash"`
		} `json:"operations"`
		Detail any `json:"detail"`
	}
	status, err := c.exchange(ctx, c.bulk, bulkMaxBody, http.MethodPost, "/api/sync/negotiate", in, &body)
	if err == nil && status == http.StatusBadRequest && fmt.Sprint(body.Detail) == "Sync is disabled for this device" {
		return domain.SyncPlan{}, errSyncDisabled
	}
	if err == nil && status == http.StatusNotFound {
		return domain.SyncPlan{}, &domain.Error{Kind: domain.KindNotFound, Message: "RomM no longer knows this Deck's Device. Pair the Deck again to sync saves."}
	}
	if err := statusError("sync saves", status, err); err != nil {
		return domain.SyncPlan{}, err
	}
	plan := domain.SyncPlan{SessionID: body.SessionID}
	for _, op := range body.Operations {
		o := domain.SyncOperation{
			Action: domain.SyncAction(op.Action), ROMID: op.ROMID, FileName: op.FileName, Slot: deref(op.Slot),
			Emulator: deref(op.Emulator), Reason: op.Reason, ServerHash: deref(op.ServerHash),
		}
		if op.SaveID != nil {
			o.SaveID = *op.SaveID
		}
		if op.ServerUpdatedAt != nil {
			o.ServerUpdatedAt = parseTime(*op.ServerUpdatedAt)
		}
		plan.Operations = append(plan.Operations, o)
	}
	return plan, nil
}

// UploadSave adds a save to its slot as the slot's newest version. It fails with
// domain.KindSaveConflict when the slot changed since this Device last synced it.
func (c *Client) UploadSave(ctx context.Context, u domain.SaveUpload) (domain.ServerSave, error) {
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	part, err := mw.CreateFormFile("saveFile", u.FileName)
	if err != nil {
		return domain.ServerSave{}, err
	}
	if _, err := part.Write(u.Data); err != nil {
		return domain.ServerSave{}, err
	}
	if err := mw.Close(); err != nil {
		return domain.ServerSave{}, err
	}
	q := url.Values{
		"rom_id":            {strconv.Itoa(u.ROMID)},
		"device_id":         {u.DeviceID},
		"content_hash":      {u.ContentHash},
		"autocleanup":       {"true"},
		"autocleanup_limit": {strconv.Itoa(saveVersions)},
	}
	// Left out, the save is in no slot; sent empty, RomM would store an empty slot name.
	if u.Slot != "" {
		q.Set("slot", u.Slot)
	}
	if u.Emulator != "" {
		q.Set("emulator", u.Emulator)
	}
	if u.Overwrite {
		q.Set("overwrite", "true")
	}
	if u.SessionID != 0 {
		q.Set("session_id", strconv.Itoa(u.SessionID))
	}
	var body wireSave
	status, err := c.send(ctx, http.MethodPost, "/api/saves?"+q.Encode(), mw.FormDataContentType(), &form, &body)
	if err == nil && status == http.StatusConflict {
		return domain.ServerSave{}, &domain.Error{Kind: domain.KindSaveConflict, Message: "RomM has a newer save since this Deck last synced."}
	}
	if err := statusError("upload the save", status, err); err != nil {
		return domain.ServerSave{}, err
	}
	return body.save(), nil
}

// SaveSlots lists the ROM with id's save slots, each with its newest save. Saves in no slot are left
// out: they're no timeline a save syncs in.
func (c *Client) SaveSlots(ctx context.Context, id int) ([]domain.SaveSlot, error) {
	var body struct {
		Slots []struct {
			Slot   *string  `json:"slot"`
			Count  int      `json:"count"`
			Latest wireSave `json:"latest"`
		} `json:"slots"`
	}
	status, err := c.do(ctx, http.MethodGet, "/api/saves/summary?rom_id="+strconv.Itoa(id), nil, &body)
	if err := statusError("list the save slots", status, err); err != nil {
		return nil, err
	}
	out := []domain.SaveSlot{}
	for _, s := range body.Slots {
		if deref(s.Slot) == "" {
			continue
		}
		out = append(out, domain.SaveSlot{Name: *s.Slot, Count: s.Count, Latest: s.Latest.save()})
	}
	return out, nil
}

// DownloadSave fetches a save's content. RomM records this Device as synced to it only once
// ConfirmSaveDownload says the bytes arrived.
func (c *Client) DownloadSave(ctx context.Context, id int, deviceID string, sessionID int) ([]byte, error) {
	q := url.Values{"device_id": {deviceID}, "optimistic": {"false"}}
	if sessionID != 0 {
		q.Set("session_id", strconv.Itoa(sessionID))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/saves/"+strconv.Itoa(id)+"/content?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		c.log.Warn("save download failed", "save_id", id, "err", err)
		return nil, noAnswer(err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, &domain.Error{Kind: domain.KindNotFound, Message: "RomM no longer has that save."}
	default:
		if err := statusError("download the save", resp.StatusCode, nil); err != nil {
			return nil, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSave+1))
	if err != nil {
		return nil, &domain.Error{Kind: domain.KindUnreachable, Message: "Couldn't reach the server.", Err: err}
	}
	if len(data) > maxSave {
		return nil, fmt.Errorf("save is over %d bytes", maxSave)
	}
	return data, nil
}

// ConfirmSaveDownload tells RomM this Device holds the save with id, whose content hashes to hash.
func (c *Client) ConfirmSaveDownload(ctx context.Context, id int, deviceID, hash string) error {
	status, err := c.do(ctx, http.MethodPost, "/api/saves/"+strconv.Itoa(id)+"/downloaded",
		map[string]string{"device_id": deviceID, "content_hash": hash}, nil)
	return statusError("confirm the save download", status, err)
}

// CompleteSync closes the session a negotiation opened, with how many of its steps worked.
func (c *Client) CompleteSync(ctx context.Context, sessionID, completed, failed int) error {
	status, err := c.do(ctx, http.MethodPost, "/api/sync/sessions/"+strconv.Itoa(sessionID)+"/complete",
		map[string]int{"operations_completed": completed, "operations_failed": failed}, nil)
	return statusError("finish the sync", status, err)
}

// noAnswer is the error of a request that got no answer: the certificate's own error when it was
// refused, else KindUnreachable.
func noAnswer(err error) error {
	if de, ok := errors.AsType[*domain.Error](err); ok {
		return de
	}
	return &domain.Error{Kind: domain.KindUnreachable, Message: "Couldn't reach the server.", Err: err}
}

// send is exchange for a body that isn't JSON, such as a multipart form.
func (c *Client) send(ctx context.Context, method, path, contentType string, in io.Reader, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, in)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	attrs := []any{"method", method, "url", c.base + strings.SplitN(path, "?", 2)[0]}
	if err != nil {
		c.log.Warn("request failed", append(attrs, "err", err)...)
		return 0, noAnswer(err)
	}
	defer func() { _ = resp.Body.Close() }()
	c.log.Debug("request", append(attrs, "status", resp.StatusCode)...)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
			return 0, &domain.Error{Kind: domain.KindUnreachable, Message: "Couldn't reach the server.", Err: err}
		}
	}
	return resp.StatusCode, nil
}

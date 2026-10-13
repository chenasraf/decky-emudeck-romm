package romm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// Platforms returns every platform on the server.
func (c *Client) Platforms(ctx context.Context) ([]domain.Platform, error) {
	var body []struct {
		ID          int    `json:"id"`
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		CustomName  string `json:"custom_name"`
		DisplayName string `json:"display_name"`
		ROMCount    int    `json:"rom_count"`
	}
	status, err := c.getBulk(ctx, "/api/platforms", &body)
	if err := statusError("list platforms", status, err); err != nil {
		return nil, err
	}
	out := make([]domain.Platform, 0, len(body))
	for _, p := range body {
		name := firstNonEmpty(p.DisplayName, p.CustomName, p.Name, p.Slug)
		out = append(out, domain.Platform{ID: p.ID, Slug: p.Slug, Name: name, ROMCount: p.ROMCount})
	}
	return out, nil
}

// romQuery turns off the indexes RomM computes over the whole library on every page by default.
const romQuery = "with_char_index=false&with_filter_values=false"

// ROMs returns up to limit ROMs from offset, ordered by id. A non-zero since returns only those
// changed after it.
func (c *Client) ROMs(ctx context.Context, since time.Time, offset, limit int) ([]domain.ROM, error) {
	q := url.Values{
		"limit":             {strconv.Itoa(limit)},
		"offset":            {strconv.Itoa(offset)},
		"order_by":          {"id"},
		"order_dir":         {"asc"},
		"with_rom_id_index": {"false"},
	}
	if !since.IsZero() {
		q.Set("updated_after", since.UTC().Format(time.RFC3339Nano))
	}
	var body struct {
		Items []struct {
			ID               int      `json:"id"`
			PlatformID       int      `json:"platform_id"`
			Name             string   `json:"name"`
			FSName           string   `json:"fs_name"`
			FSExtension      string   `json:"fs_extension"`
			FSSizeBytes      int64    `json:"fs_size_bytes"`
			Regions          []string `json:"regions"`
			Revision         string   `json:"revision"`
			HasMultipleFiles bool     `json:"has_multiple_files"`
			UpdatedAt        string   `json:"updated_at"`
			PathCoverSmall   string   `json:"path_cover_small"`
			PathCoverLarge   string   `json:"path_cover_large"`
		} `json:"items"`
	}
	status, err := c.getBulk(ctx, "/api/roms?"+romQuery+"&"+q.Encode(), &body)
	if err := statusError("list ROMs", status, err); err != nil {
		return nil, err
	}
	out := make([]domain.ROM, 0, len(body.Items))
	for _, r := range body.Items {
		updated, _ := time.Parse(time.RFC3339Nano, r.UpdatedAt)
		out = append(out, domain.ROM{
			ID:         r.ID,
			PlatformID: r.PlatformID,
			Name:       firstNonEmpty(r.Name, r.FSName),
			FileName:   r.FSName,
			Extension:  r.FSExtension,
			SizeBytes:  r.FSSizeBytes,
			Regions:    r.Regions,
			Revision:   r.Revision,
			MultiFile:  r.HasMultipleFiles,
			UpdatedAt:  updated.UTC(),
			CoverSmall: r.PathCoverSmall,
			CoverLarge: r.PathCoverLarge,
		})
	}
	return out, nil
}

// SearchROMs returns the ids of up to limit ROMs matching term, most relevant first. RomM matches
// term against names and file names, and against CRC32, MD5 and SHA-1 hashes.
func (c *Client) SearchROMs(ctx context.Context, term string, limit int) ([]int, error) {
	q := url.Values{
		"search_term":       {term},
		"limit":             {strconv.Itoa(limit)},
		"with_rom_id_index": {"false"},
	}
	var body struct {
		Items []struct {
			ID int `json:"id"`
		} `json:"items"`
	}
	// Each item carries its full metadata, so a page of results outgrows maxBody; without
	// order_by RomM orders them by relevance.
	status, err := c.exchange(ctx, c.http, bulkMaxBody, http.MethodGet, "/api/roms?"+romQuery+"&"+q.Encode(), nil, &body)
	if err := statusError("search ROMs", status, err); err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(body.Items))
	for _, r := range body.Items {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// ROMIDs returns the id of every ROM on the server, which a delta sync compares against to find
// deleted ones.
func (c *Client) ROMIDs(ctx context.Context) ([]int, error) {
	var body struct {
		IDs []int `json:"rom_id_index"`
	}
	status, err := c.getBulk(ctx, "/api/roms?"+romQuery+"&limit=1&with_rom_id_index=true", &body)
	if err := statusError("list ROM ids", status, err); err != nil {
		return nil, err
	}
	if body.IDs == nil {
		body.IDs = []int{}
	}
	return body.IDs, nil
}

// statusError maps an authenticated GET's outcome to an error, nil on 200.
func statusError(what string, status int, err error) error {
	switch {
	case err != nil:
		return err
	case status == http.StatusOK:
		return nil
	case status == http.StatusUnauthorized:
		return errSignedOut
	case status == http.StatusForbidden:
		return &domain.Error{Kind: domain.KindMissingPermission, Message: "RomM didn't grant the plugin permission to " + what + "."}
	default:
		return fmt.Errorf("%s: unexpected status %d", what, status)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

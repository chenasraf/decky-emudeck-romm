package romm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// maxCover bounds a cover image; RomM's large covers are a few hundred KB.
const maxCover = 16 << 20

// Cover fetches the cover image at path, which RomM gives relative to the server. An absolute URL
// is fetched as it is.
func (c *Client) Cover(ctx context.Context, path string) ([]byte, error) {
	target := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		target = c.base + "/" + strings.TrimLeft(path, "/")
	}
	// RomM's cache-busting ?ts= is a raw timestamp with a space in it, which isn't valid in a
	// request line.
	target = strings.ReplaceAll(target, " ", "%20")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	// RomM serves covers as static files without auth; the token goes only to the server itself.
	if strings.HasPrefix(target, c.base+"/") && c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.log.Warn("cover failed", "url", target, "err", err)
		var de *domain.Error
		if errors.As(err, &de) {
			return nil, de
		}
		return nil, &domain.Error{Kind: domain.KindUnreachable, Message: "Couldn't reach the server.", Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, &domain.Error{Kind: domain.KindNotFound, Message: "RomM has no cover there."}
	case resp.StatusCode >= http.StatusInternalServerError:
		return nil, &domain.Error{Kind: domain.KindUnreachable, Message: "Couldn't reach the server.", Err: fmt.Errorf("cover: status %d", resp.StatusCode)}
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("cover: unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCover+1))
	if err != nil {
		return nil, &domain.Error{Kind: domain.KindUnreachable, Message: "Couldn't reach the server.", Err: err}
	}
	if len(data) > maxCover {
		return nil, fmt.Errorf("cover is over %d bytes", maxCover)
	}
	c.log.Debug("cover", "url", target, "bytes", len(data))
	return data, nil
}

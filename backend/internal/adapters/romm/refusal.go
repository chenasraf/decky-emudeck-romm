package romm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// refusalPeek bounds how much of a refused response's body is read, to tell who sent it and to
// log.
const refusalPeek = 4 << 10

// refusalLogged bounds the start of a refused response's body that goes in the log.
const refusalLogged = 200

// refusal is a non-success response, told apart by who sent it. RomM's API explains every refusal
// in a JSON detail; nginx in RomM's image, or a proxy in front of it, answers with a page of its
// own. A 403 from nginx means it couldn't read the file it was asked to serve.
type refusal struct {
	status      int
	server      string
	contentType string
	// api is set when the body is RomM's API explaining the refusal.
	api bool
	// detail is the reason RomM's API gave, and body the start of the response; both are empty
	// once withheld.
	detail string
	body   string
}

func newRefusal(resp *http.Response, body []byte) refusal {
	r := refusal{
		status:      resp.StatusCode,
		server:      resp.Header.Get("Server"),
		contentType: resp.Header.Get("Content-Type"),
		body:        logSnippet(body),
	}
	var api struct {
		Detail any `json:"detail"`
	}
	if json.Unmarshal(body, &api) == nil && api.Detail != nil {
		r.api = true
		r.detail = fmt.Sprint(api.Detail)
	}
	return r
}

// withheld drops what the server said, keeping who said it. A refused request's body can carry a
// password or a pairing code, and FastAPI's validation errors echo the input back.
func (r refusal) withheld() refusal {
	r.detail, r.body = "", ""
	return r
}

// readRefusal reads the start of resp's body into a refusal.
func readRefusal(resp *http.Response) refusal {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, refusalPeek))
	return newRefusal(resp, body)
}

func (r refusal) fromAPI() bool { return r.api }

func (r refusal) attrs() []any {
	return []any{"status", r.status, "server", r.server, "content_type", r.contentType, "from_api", r.api, "detail", r.detail, "body", r.body}
}

func (r refusal) String() string {
	from := "not from RomM's API"
	switch {
	case r.api && r.detail != "":
		from = "RomM: " + r.detail
	case r.api:
		from = "from RomM's API"
	}
	if r.server != "" {
		return fmt.Sprintf("status %d from %s (%s)", r.status, r.server, from)
	}
	return fmt.Sprintf("status %d (%s)", r.status, from)
}

// contentRefused maps a refused file download to an error, logging who refused it.
func (c *Client) contentRefused(url string, resp *http.Response) error {
	r := readRefusal(resp)
	c.log.Warn("download refused", append([]any{"url", url}, r.attrs()...)...)
	switch {
	case r.status == http.StatusUnauthorized:
		return errSignedOut
	case r.status == http.StatusForbidden && r.fromAPI():
		return &domain.Error{Kind: domain.KindMissingPermission, Message: "RomM didn't grant the plugin permission to download the game."}
	case r.status == http.StatusForbidden:
		return &domain.Error{
			Kind:    domain.KindServerRefused,
			Message: "The server refused to send the game's file without RomM saying why. RomM's web server does this when it can't read the file in its library folder, so check that folder's permissions on the server.",
			Err:     fmt.Errorf("download: %s", r),
		}
	default:
		return fmt.Errorf("download the game: unexpected %s", r)
	}
}

// logSnippet is the start of body on one line, for the log.
func logSnippet(body []byte) string {
	s := strings.Join(strings.Fields(strings.ToValidUTF8(string(body), "?")), " ")
	if len(s) > refusalLogged {
		s = strings.ToValidUTF8(s[:refusalLogged], "") + "…"
	}
	return s
}

// Package romm talks to a RomM server's HTTP API.
package romm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const (
	dialTimeout    = 5 * time.Second
	requestTimeout = 10 * time.Second
	maxBody        = 1 << 20
	// Catalog pages are large: RomM sends every ROM with its full metadata, about 15 KB each, and
	// has no way to ask for fewer fields.
	bulkTimeout = 2 * time.Minute
	bulkMaxBody = 128 << 20
)

// clientName is how the plugin names itself to RomM, in the User-Agent and on its Device.
const clientName = "decky-emudeck-romm"

type Client struct {
	base      string
	token     string
	version   string
	userAgent string
	http      *http.Client
	// bulk is http with room for catalog pages.
	bulk *http.Client
	// download has no timeout; DownloadFile watches for stalls instead.
	download *http.Client
	log      *slog.Logger
}

// WithLogger makes the client log each request at debug level, and failed ones at warn.
func (c *Client) WithLogger(log *slog.Logger) *Client {
	c.log = log
	return c
}

// New returns a client for server. token may be empty for the unauthenticated endpoints, and
// version is the plugin's.
func New(server domain.Server, token, version string) *Client {
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: dialTimeout}).DialContext,
		TLSHandshakeTimeout: dialTimeout,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// Verification happens in VerifyConnection, which also accepts the pinned certificate.
			InsecureSkipVerify: true, //nolint:gosec
			VerifyConnection:   verifyConnection(server.CertFingerprint),
		},
	}
	return &Client{
		base:    strings.TrimRight(server.URL, "/"),
		token:   token,
		version: version,
		// Go's default User-Agent gets requests blocked by Cloudflare's bot protection before
		// they reach RomM.
		userAgent: clientName + "/" + version,
		http:      &http.Client{Transport: transport, Timeout: requestTimeout},
		bulk:      &http.Client{Transport: transport, Timeout: bulkTimeout},
		download:  &http.Client{Transport: transport},
		log:       slog.New(slog.DiscardHandler),
	}
}

// Fingerprint is the SHA-256 of a certificate's DER bytes, as colon-separated uppercase hex.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// verifyConnection accepts a certificate the system trusts, or one whose fingerprint is pin. The
// system check comes first so a server that moves to a publicly trusted certificate keeps working.
func verifyConnection(pin string) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("server sent no certificate")
		}
		leaf := cs.PeerCertificates[0]
		intermediates := x509.NewCertPool()
		for _, c := range cs.PeerCertificates[1:] {
			intermediates.AddCert(c)
		}
		_, verifyErr := leaf.Verify(x509.VerifyOptions{DNSName: cs.ServerName, Intermediates: intermediates})
		if verifyErr == nil {
			return nil
		}
		fp := Fingerprint(leaf)
		if pin != "" && strings.EqualFold(pin, fp) {
			return nil
		}
		return &domain.Error{
			Kind:        domain.KindUntrustedCert,
			Message:     "The server's certificate isn't trusted.",
			Fingerprint: fp,
			Err:         verifyErr,
		}
	}
}

// Heartbeat returns the version of RomM the server runs.
func (c *Client) Heartbeat(ctx context.Context) (string, error) {
	var body struct {
		System struct {
			Version string `json:"VERSION"`
		} `json:"SYSTEM"`
	}
	status, err := c.do(ctx, http.MethodGet, "/api/heartbeat", nil, &body)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK || body.System.Version == "" {
		return "", &domain.Error{Kind: domain.KindNotRomM, Message: "No RomM server answered at this address."}
	}
	return body.System.Version, nil
}

// errSignedOut is what any request made with the client's token returns on a 401: client tokens
// don't refresh, so RomM revoked it or it expired.
var errSignedOut = &domain.Error{Kind: domain.KindSignedOut, Message: "RomM no longer accepts this Deck's sign-in. Sign in again."}

// CheckToken asks RomM whether it still accepts the client's token.
func (c *Client) CheckToken(ctx context.Context) error {
	status, err := c.do(ctx, http.MethodGet, "/api/users/me", nil, nil)
	if err != nil {
		return err
	}
	switch status {
	// A 403 means the token is valid but lacks me.read, which the user may narrow away when
	// approving a pairing.
	case http.StatusOK, http.StatusForbidden:
		return nil
	case http.StatusUnauthorized:
		return errSignedOut
	default:
		return fmt.Errorf("check token: unexpected status %d", status)
	}
}

var errRateLimited = &domain.Error{Kind: domain.KindRateLimited, Message: "Too many pairing attempts. Wait a minute and try again."}

// ExchangePairCode redeems a pair code for a client token. RomM regenerates the token's secret on
// exchange, so any copy of that token made earlier stops working. The result has no Server.
func (c *Client) ExchangePairCode(ctx context.Context, code string) (domain.SignIn, error) {
	var body struct {
		RawToken string   `json:"raw_token"`
		Scopes   []string `json:"scopes"`
		DeviceID *string  `json:"device_id"`
	}
	status, err := c.do(ctx, http.MethodPost, "/api/client-tokens/exchange", map[string]string{"code": code}, &body)
	if err != nil {
		return domain.SignIn{}, err
	}
	switch {
	case status == http.StatusOK && body.RawToken != "":
		in := domain.SignIn{Token: body.RawToken, Scopes: body.Scopes}
		if body.DeviceID != nil {
			in.DeviceID = *body.DeviceID
		}
		return in, nil
	case status == http.StatusNotFound:
		return domain.SignIn{}, &domain.Error{Kind: domain.KindInvalidPairCode, Message: "That pair code is wrong or has expired. Make a new one in RomM."}
	case status == http.StatusForbidden:
		return domain.SignIn{}, &domain.Error{Kind: domain.KindPairRejected, Message: "RomM refused the pair code: the account that made it is disabled."}
	case status == http.StatusTooManyRequests:
		return domain.SignIn{}, errRateLimited
	default:
		return domain.SignIn{}, fmt.Errorf("pair code exchange: unexpected status %d", status)
	}
}

// StartDeviceAuth asks RomM to pair this Deck as a Device, for the user to approve in RomM's web
// UI.
func (c *Client) StartDeviceAuth(ctx context.Context, device domain.DeviceIdentity, scopes []string) (domain.DevicePairing, error) {
	req := map[string]any{
		"client_device_identifier": device.Identifier,
		"name":                     device.Name,
		"client":                   clientName,
		"platform":                 device.Platform,
		"client_version":           c.version,
		"requested_scopes":         scopes,
	}
	var body struct {
		DeviceCode               string `json:"device_code"`
		UserCode                 string `json:"user_code"`
		VerificationPathComplete string `json:"verification_path_complete"`
		ExpiresIn                int    `json:"expires_in"`
		Interval                 int    `json:"interval"`
	}
	status, err := c.do(ctx, http.MethodPost, "/api/auth/device/init", req, &body)
	if err != nil {
		return domain.DevicePairing{}, err
	}
	switch status {
	case http.StatusCreated, http.StatusOK:
		return domain.DevicePairing{
			DeviceCode:      body.DeviceCode,
			UserCode:        body.UserCode,
			VerificationURL: c.base + body.VerificationPathComplete,
			ExpiresIn:       time.Duration(body.ExpiresIn) * time.Second,
			Interval:        time.Duration(body.Interval) * time.Second,
		}, nil
	case http.StatusTooManyRequests:
		return domain.DevicePairing{}, errRateLimited
	default:
		return domain.DevicePairing{}, fmt.Errorf("start device pairing: unexpected status %d", status)
	}
}

// PollDeviceAuth checks once whether the user has approved a pairing. It returns the token, or one
// of the domain.ErrPairing errors while there is none. The result has no Server.
func (c *Client) PollDeviceAuth(ctx context.Context, deviceCode string) (domain.SignIn, error) {
	var body struct {
		AccessToken string   `json:"access_token"`
		DeviceID    string   `json:"device_id"`
		Scopes      []string `json:"scopes"`
		Detail      string   `json:"detail"`
	}
	status, err := c.do(ctx, http.MethodPost, "/api/auth/device/token", map[string]string{"device_code": deviceCode}, &body)
	if err != nil {
		return domain.SignIn{}, err
	}
	switch {
	case status == http.StatusOK && body.AccessToken != "":
		return domain.SignIn{Token: body.AccessToken, DeviceID: body.DeviceID, Scopes: body.Scopes}, nil
	case status == http.StatusTooManyRequests:
		return domain.SignIn{}, domain.ErrPairingSlowDown
	case status != http.StatusBadRequest:
		return domain.SignIn{}, fmt.Errorf("poll device pairing: unexpected status %d", status)
	}
	switch body.Detail {
	case "authorization_pending":
		return domain.SignIn{}, domain.ErrPairingPending
	case "slow_down":
		return domain.SignIn{}, domain.ErrPairingSlowDown
	case "access_denied":
		return domain.SignIn{}, domain.ErrPairingDenied
	case "expired_token":
		return domain.SignIn{}, domain.ErrPairingExpired
	default:
		return domain.SignIn{}, fmt.Errorf("poll device pairing: unexpected error %q", body.Detail)
	}
}

// PasswordGrant signs in with a username and password and returns a short-lived OAuth access
// token limited to scopes. RomM refuses the whole grant when the account lacks any of them.
func (c *Client) PasswordGrant(ctx context.Context, username, password string, scopes []string) (string, error) {
	form := url.Values{
		"grant_type": {"password"},
		"username":   {username},
		"password":   {password},
		"scope":      {strings.Join(scopes, " ")},
	}
	var body struct {
		AccessToken string `json:"access_token"`
		Detail      string `json:"detail"`
	}
	status, err := c.do(ctx, http.MethodPost, "/api/token", form, &body)
	if err != nil {
		return "", err
	}
	switch {
	case status == http.StatusOK && body.AccessToken != "":
		return body.AccessToken, nil
	case status == http.StatusUnauthorized:
		return "", &domain.Error{Kind: domain.KindBadCredentials, Message: "Wrong username or password."}
	case status == http.StatusForbidden && body.Detail == "Insufficient scope":
		return "", &domain.Error{
			Kind:    domain.KindMissingPermission,
			Message: "Your RomM account lacks the me.write permission, which signing in a device needs. Ask your RomM admin to grant it.",
		}
	case status == http.StatusForbidden:
		return "", &domain.Error{Kind: domain.KindAccountDisabled, Message: "This RomM account is disabled."}
	case status == http.StatusTooManyRequests:
		return "", &domain.Error{Kind: domain.KindRateLimited, Message: "Too many sign-in attempts. Wait a minute and try again."}
	default:
		return "", fmt.Errorf("password sign-in: unexpected status %d", status)
	}
}

// DeviceAuthScopes returns the scopes of a pending device pairing that the signed-in account may
// grant: the ones it asked for, narrowed to what the account has.
func (c *Client) DeviceAuthScopes(ctx context.Context, userCode string) ([]string, error) {
	var body struct {
		AllowedScopes []string `json:"allowed_scopes"`
	}
	status, err := c.do(ctx, http.MethodGet, "/api/auth/device/pending/"+url.PathEscape(userCode), nil, &body)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("read device pairing: unexpected status %d", status)
	}
	return body.AllowedScopes, nil
}

// ApproveDeviceAuth approves a pending device pairing as the signed-in account, granting scopes
// with a token that never expires. RomM creates or reuses the Device, as when the user approves
// in its web UI.
func (c *Client) ApproveDeviceAuth(ctx context.Context, userCode string, scopes []string) error {
	req := map[string]any{"user_code": userCode, "approved_scopes": scopes, "expires_in": "never"}
	var body struct {
		Detail any `json:"detail"`
	}
	status, err := c.do(ctx, http.MethodPost, "/api/auth/device/approve", req, &body)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("approve device pairing: unexpected status %d: %v", status, body.Detail)
	}
	return nil
}

// do sends a request and decodes its JSON response into out. A non-2xx status is not an error
// here; callers map statuses to what they mean for their endpoint. Error bodies are decoded too,
// for endpoints that explain a 4xx in them.
func (c *Client) do(ctx context.Context, method, path string, in, out any) (int, error) {
	return c.exchange(ctx, c.http, maxBody, method, path, in, out)
}

// getBulk is do for a GET whose response can run to many megabytes.
func (c *Client) getBulk(ctx context.Context, path string, out any) (int, error) {
	return c.exchange(ctx, c.bulk, bulkMaxBody, http.MethodGet, path, nil, out)
}

func (c *Client) exchange(ctx context.Context, client *http.Client, limit int64, method, path string, in, out any) (int, error) {
	var body io.Reader
	contentType := "application/json"
	switch v := in.(type) {
	case nil:
	case url.Values:
		body = strings.NewReader(v.Encode())
		contentType = "application/x-www-form-urlencoded"
	default:
		data, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	start := time.Now()
	resp, err := client.Do(req)
	// Tokens and codes travel in headers and bodies, never in the URL, so the URL is safe to log.
	attrs := []any{"method", method, "url", c.base + path, "duration", time.Since(start).Round(time.Millisecond)}
	if err != nil {
		c.log.Warn("request failed", append(attrs, "err", err)...)
		if de, ok := errors.AsType[*domain.Error](err); ok {
			return 0, de
		}
		return 0, &domain.Error{Kind: domain.KindUnreachable, Message: "Couldn't reach the server.", Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	c.log.Debug("request", append(attrs, "status", resp.StatusCode)...)

	if out == nil {
		return resp.StatusCode, nil
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, limit)).Decode(out)
	if err != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// A body cut off by a timeout or a dropped connection is the network's doing, not a sign
		// that something other than RomM answered.
		var netErr net.Error
		if errors.As(err, &netErr) || errors.Is(err, io.ErrUnexpectedEOF) || ctx.Err() != nil {
			c.log.Warn("response cut off", append(attrs, "err", err)...)
			return 0, &domain.Error{Kind: domain.KindUnreachable, Message: "Couldn't reach the server.", Err: err}
		}
		return 0, &domain.Error{Kind: domain.KindNotRomM, Message: "No RomM server answered at this address.", Err: err}
	}
	// An error body that isn't JSON leaves out empty, which callers read as "no detail".
	return resp.StatusCode, nil
}

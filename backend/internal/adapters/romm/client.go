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
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const (
	dialTimeout    = 5 * time.Second
	requestTimeout = 10 * time.Second
	maxBody        = 1 << 20
)

type Client struct {
	base      string
	token     string
	userAgent string
	http      *http.Client
}

// New returns a client for server. token may be empty for the unauthenticated endpoints.
// userAgent replaces Go's default, which Cloudflare's bot protection blocks before requests reach
// RomM.
func New(server domain.Server, token, userAgent string) *Client {
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
		base:      strings.TrimRight(server.URL, "/"),
		token:     token,
		userAgent: userAgent,
		http:      &http.Client{Transport: transport, Timeout: requestTimeout},
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

// ExchangePairCode redeems a pair code for a client token. RomM regenerates the token's secret on
// exchange, so any copy of that token made earlier stops working.
func (c *Client) ExchangePairCode(ctx context.Context, code string) (string, error) {
	var body struct {
		RawToken string `json:"raw_token"`
	}
	status, err := c.do(ctx, http.MethodPost, "/api/client-tokens/exchange", map[string]string{"code": code}, &body)
	if err != nil {
		return "", err
	}
	switch {
	case status == http.StatusOK && body.RawToken != "":
		return body.RawToken, nil
	case status == http.StatusNotFound:
		return "", &domain.Error{Kind: domain.KindInvalidPairCode, Message: "That pair code is wrong or has expired. Pair codes last one minute; make a new one in RomM."}
	case status == http.StatusForbidden:
		return "", &domain.Error{Kind: domain.KindPairRejected, Message: "RomM refused the pair code: the account that made it is disabled."}
	case status == http.StatusTooManyRequests:
		return "", &domain.Error{Kind: domain.KindRateLimited, Message: "Too many pairing attempts. Wait a minute and try again."}
	default:
		return "", fmt.Errorf("pair code exchange: unexpected status %d", status)
	}
}

// do sends a request and decodes a JSON response into out. A non-2xx status is not an error here;
// callers map statuses to what they mean for their endpoint.
func (c *Client) do(ctx context.Context, method, path string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
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
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		var de *domain.Error
		if errors.As(err, &de) {
			return 0, de
		}
		return 0, &domain.Error{Kind: domain.KindUnreachable, Message: "Couldn't reach the server.", Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 && out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
			return 0, &domain.Error{Kind: domain.KindNotRomM, Message: "No RomM server answered at this address.", Err: err}
		}
	}
	return resp.StatusCode, nil
}

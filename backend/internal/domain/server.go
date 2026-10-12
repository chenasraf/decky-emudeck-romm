package domain

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// MinServerVersion is the oldest RomM release the plugin works with: 5.1.0 is the first that can
// resume multi-file downloads.
var MinServerVersion = [3]int{5, 1, 0}

// Server is the RomM instance the plugin is paired with.
type Server struct {
	URL string
	// CertFingerprint is the SHA-256 of a certificate the user chose to trust although it doesn't
	// verify. Empty when the server's certificate verifies, or the server is plain HTTP.
	CertFingerprint string
}

// ServerURLCandidates turns what the user typed into the URLs to try, in order. Without a scheme
// HTTPS comes first, then HTTP.
func ServerURLCandidates(input string) ([]string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, &Error{Kind: KindInvalidInput, Message: "Enter your RomM server's address."}
	}
	explicit := strings.Contains(input, "://")
	if !explicit {
		input = "https://" + input
	}
	u, err := url.Parse(input)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return nil, &Error{Kind: KindInvalidInput, Message: "That doesn't look like a server address."}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, &Error{Kind: KindInvalidInput, Message: "The address must start with http:// or https://."}
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, &Error{Kind: KindInvalidInput, Message: "Enter only the server's address, without a login, query or #."}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""

	if explicit {
		return []string{u.String()}, nil
	}
	plain := *u
	plain.Scheme = "http"
	return []string{u.String(), plain.String()}, nil
}

// CheckServerVersion returns an error unless version is MinServerVersion or newer. Pre-release
// suffixes are ignored: 5.1.0-beta.1 counts as 5.1.0.
func CheckServerVersion(version string) error {
	got, err := parseVersion(version)
	if err != nil || compareVersions(got, MinServerVersion) < 0 {
		return &Error{
			Kind:    KindUnsupportedVersion,
			Message: fmt.Sprintf("This server runs RomM %s. The plugin needs RomM %d.%d.%d or newer.", version, MinServerVersion[0], MinServerVersion[1], MinServerVersion[2]),
			Version: version,
		}
	}
	return nil
}

func parseVersion(v string) ([3]int, error) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, errors.New("not a release version")
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, errors.New("not a release version")
		}
		out[i] = n
	}
	return out, nil
}

func compareVersions(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			return a[i] - b[i]
		}
	}
	return 0
}

// pairCodeAlphabet is the characters RomM draws pair codes from; it leaves out 0, O, 1, I and L.
const pairCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

const pairCodeLength = 8

// NormalizePairCode accepts a pair code as the user typed it (any case, with dashes or spaces) and
// returns it the way RomM stores it.
func NormalizePairCode(input string) (string, error) {
	code := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(input)))
	if len(code) != pairCodeLength || strings.Trim(code, pairCodeAlphabet) != "" {
		return "", &Error{Kind: KindInvalidInput, Message: "A pair code is 8 letters and digits, as shown in RomM."}
	}
	return code, nil
}

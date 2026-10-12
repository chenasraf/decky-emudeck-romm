package domain

import (
	"errors"
	"slices"
	"testing"
)

func TestServerURLCandidates(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"romm.example.com", []string{"https://romm.example.com", "http://romm.example.com"}},
		{"  romm.lan:8080/ ", []string{"https://romm.lan:8080", "http://romm.lan:8080"}},
		{"192.168.1.5:3000/romm/", []string{"https://192.168.1.5:3000/romm", "http://192.168.1.5:3000/romm"}},
		{"http://romm.lan", []string{"http://romm.lan"}},
		{"https://romm.example.com/", []string{"https://romm.example.com"}},
	}
	for _, tt := range tests {
		got, err := ServerURLCandidates(tt.input)
		if err != nil {
			t.Errorf("%q: %v", tt.input, err)
			continue
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%q: got %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestServerURLCandidatesRejects(t *testing.T) {
	for _, input := range []string{"", "   ", "ftp://romm.lan", "https://", "https://user:pw@romm.lan", "romm.lan?x=1", "romm.lan#top"} {
		_, err := ServerURLCandidates(input)
		var de *Error
		if !errors.As(err, &de) || de.Kind != KindInvalidInput {
			t.Errorf("%q: want invalid_input, got %v", input, err)
		}
	}
}

func TestCheckServerVersion(t *testing.T) {
	for _, v := range []string{"5.1.0", "5.4.0", "v5.1.2", "6.0.0", "5.10.0", "5.1.0-beta.1"} {
		if err := CheckServerVersion(v); err != nil {
			t.Errorf("%s: unexpected error %v", v, err)
		}
	}
	for _, v := range []string{"5.0.9", "4.8.1", "development", "", "5.1"} {
		err := CheckServerVersion(v)
		var de *Error
		if !errors.As(err, &de) || de.Kind != KindUnsupportedVersion || de.Version != v {
			t.Errorf("%q: want unsupported_version carrying the version, got %v", v, err)
		}
	}
}

func TestNormalizePairCode(t *testing.T) {
	for input, want := range map[string]string{
		"ABCD2345":   "ABCD2345",
		"abcd-2345":  "ABCD2345",
		" ab cd 23 ": "",
		"abcd 2345":  "ABCD2345",
	} {
		got, err := NormalizePairCode(input)
		if want == "" {
			if err == nil {
				t.Errorf("%q: want error, got %q", input, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"ABCD234", "ABCD23456", "ABCD0123", "OOOOIIII", "ABCD234L"} {
		if _, err := NormalizePairCode(input); err == nil {
			t.Errorf("%q: want error", input)
		}
	}
}

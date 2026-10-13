package domain

import (
	"slices"
	"testing"
)

func TestRequestedScopesCoverEveryFeature(t *testing.T) {
	if got := DisabledFeatures(RequestedScopes()); len(got) != 0 {
		t.Fatalf("requested scopes leave features disabled: %+v", got)
	}
	scopes := RequestedScopes()
	for i, s := range scopes {
		if slices.Contains(scopes[i+1:], s) {
			t.Fatalf("duplicate scope %s in %v", s, scopes)
		}
	}
}

func TestDisabledFeatures(t *testing.T) {
	if got := DisabledFeatures(nil); got != nil {
		t.Fatalf("unknown scopes disabled %+v", got)
	}

	got := DisabledFeatures([]string{"me.read", "platforms.read", "roms.read", "collections.read", "assets.read"})
	want := []DisabledFeature{
		{Feature: FeatureBIOS, MissingScopes: []string{"firmware.read"}},
		{Feature: FeatureSaveSync, MissingScopes: []string{"assets.write", "devices.read", "devices.write"}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Feature != want[i].Feature || !slices.Equal(got[i].MissingScopes, want[i].MissingScopes) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
}

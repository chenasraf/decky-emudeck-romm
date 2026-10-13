package domain

import "testing"

func TestConversionFor(t *testing.T) {
	if c, ok := system(t, "3ds").ConversionFor("Zelda (USA).CIA"); !ok || c.To != "cci" {
		t.Fatalf("3ds .cia: %+v, %v", c, ok)
	}
	if _, ok := system(t, "3ds").ConversionFor("Zelda (USA).cci"); ok {
		t.Fatal("a .cci needs no conversion")
	}
	if c, ok := system(t, "xbox").ConversionFor("Halo.iso"); !ok || c.To != "xiso" {
		t.Fatalf("xbox .iso: %+v, %v", c, ok)
	}
	if _, ok := system(t, "ps2").ConversionFor("Game.iso"); ok {
		t.Fatal("a PS2 .iso plays as it is")
	}
}

func TestInstallNote(t *testing.T) {
	if got := system(t, "3ds").InstallNote("Zelda.cia", "amd64"); got != "Needs installing in Azahar: a .cia file doesn't play as it is." {
		t.Fatalf("note %q", got)
	}
	if got := system(t, "3ds").InstallNote("Zelda.cci", "amd64"); got != "" {
		t.Fatalf("note for a playable file %q", got)
	}
}

package domain

import (
	"bytes"
	"testing"
)

func TestPSXBootExe(t *testing.T) {
	cases := map[string]string{
		"BOOT = cdrom:\\SLUS_007.13;1\r\nTCB = 4\r\n": "SLUS_007.13",
		"boot=cdrom:\\\\SCES_123.45;1":                "SCES_123.45",
		"BO OT\t= cdrom:\\DATA\\MAIN.EXE;1\n":         `DATA\MAIN.EXE`,
		"TCB = 4\nEVENT = 10\n":                       PSXFallbackExe,
		"":                                            PSXFallbackExe,
		"BOOT=cdrom:SLPS_015.81":                      "SLPS_015.81",
	}
	for cnf, want := range cases {
		if got := PSXBootExe([]byte(cnf)); got != want {
			t.Errorf("%q: got %q, want %q", cnf, got, want)
		}
	}
}

func TestPSXSerial(t *testing.T) {
	cases := map[string]string{
		"SLUS_007.13":      "SLUS-00713",
		"scus_941.63":      "SCUS-94163",
		`DATA\SLPS_015.81`: "SLPS-01581",
		PSXFallbackExe:     "",
	}
	for exe, want := range cases {
		if got := PSXSerial(exe); got != want {
			t.Errorf("%q: got %q, want %q", exe, got, want)
		}
	}
}

func TestDuckStationCard(t *testing.T) {
	none := func(string) bool { return false }
	if got := (DuckStationGame{Title: "Crash Bandicoot (USA)"}).Card(none); got != "Crash Bandicoot (USA)_1.mcd" {
		t.Fatalf("one disc: %q", got)
	}
	set := DuckStationGame{Title: "Final Fantasy VII (USA) (Disc 2)", DiscSet: "Final Fantasy VII (USA)"}
	if got := set.Card(none); got != "Final Fantasy VII (USA)_1.mcd" {
		t.Fatalf("disc set: %q", got)
	}
	// A card of the disc's own, from before it was in a set, is the one DuckStation keeps using.
	own := func(name string) bool { return name == "Final Fantasy VII (USA) (Disc 2)_1.mcd" }
	if got := set.Card(own); got != "Final Fantasy VII (USA) (Disc 2)_1.mcd" {
		t.Fatalf("disc's own card: %q", got)
	}
	if got := DuckStationCardName("AC/DC * Live\x01: \"Rock\""); got != "AC_DC _ Live_: \"Rock\"_1.mcd" {
		t.Fatalf("sanitised: %q", got)
	}
}

func TestIsPSXCard(t *testing.T) {
	card := make([]byte, PSXCardSize)
	copy(card, "MC")
	if !IsPSXCard(card) || IsPSXCard(card[:1024]) || IsPSXCard(bytes.Repeat([]byte{0}, PSXCardSize)) {
		t.Fatal("card check")
	}
}

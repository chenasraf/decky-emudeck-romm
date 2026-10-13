package domain

import "testing"

func TestRunningEmulator(t *testing.T) {
	tests := map[string]struct {
		procs []Process
		want  string
	}{
		"flatpak by scope": {
			procs: []Process{{Args: []string{"/app/bin/retroarch", "-L", "core.so"}, Cgroup: "0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-flatpak-org.libretro.RetroArch-4242.scope\n"}},
			want:  "retroarch",
		},
		"flatpak by command line": {
			procs: []Process{{Args: []string{"/usr/bin/flatpak", "run", "org.DolphinEmu.dolphin-emu", "-b", "-e", "game.rvz"}}},
			want:  "dolphin",
		},
		"appimage": {
			procs: []Process{{Args: []string{"/home/deck/Applications/pcsx2-Qt.AppImage", "-batch", "game.chd"}}},
			want:  "pcsx2",
		},
		"binary": {
			procs: []Process{{Args: []string{"/home/deck/Applications/publish/Ryujinx", "game.nsp"}}},
			want:  "ryujinx",
		},
		"proton": {
			procs: []Process{{Args: []string{`Z:\home\deck\Emulation\roms\xbox360\xenia_canary.exe`, "game.iso"}}},
			want:  "xenia",
		},
		"launcher script alone": {
			procs: []Process{{Args: []string{"bash", "/home/deck/Emulation/tools/launchers/pcsx2-qt.sh"}}},
		},
		"another flatpak": {
			procs: []Process{{Args: []string{"/app/bin/firefox"}, Cgroup: "0::/app.slice/app-flatpak-org.mozilla.firefox-1.scope\n"}},
		},
		"app id as part of a path": {
			procs: []Process{{Args: []string{"ls", "/var/lib/flatpak/app/org.libretro.RetroArch"}}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			e, ok := RunningEmulator(tt.procs)
			if ok != (tt.want != "") || e.ID != tt.want {
				t.Fatalf("got %q, %v; want %q", e.ID, ok, tt.want)
			}
		})
	}
}

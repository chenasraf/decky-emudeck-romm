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

func TestRunningESDE(t *testing.T) {
	procs := []Process{
		{PID: 900, Args: []string{"/usr/bin/steam"}},
		{PID: 1210, Args: []string{"/tmp/.mount_ES-DEabc/usr/bin/es-de", "--home", "/home/deck"}},
		{PID: 1200, Args: []string{"/home/deck/Applications/ES-DE.AppImage"}},
	}
	if pid, ok := RunningESDE(procs); !ok || pid != 1200 {
		t.Fatalf("pid %d, %v", pid, ok)
	}
	if pid, ok := RunningESDE(procs[2:]); !ok || pid != 1200 {
		t.Fatalf("AppImage alone: %d, %v", pid, ok)
	}
	old := []Process{{PID: 7, Args: []string{"/home/deck/Applications/EmulationStation-DE-x64_SteamDeck.AppImage"}}}
	if _, ok := RunningESDE(old); !ok {
		t.Fatal("older EmulationStation-DE AppImage not found")
	}
	if _, ok := RunningESDE(procs[:1]); ok {
		t.Fatal("found ES-DE where none runs")
	}
}

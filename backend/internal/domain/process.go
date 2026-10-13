package domain

import (
	"path"
	"slices"
	"strings"
)

// Process is a running process, as /proc describes it.
type Process struct {
	PID  int
	Args []string
	// Cgroup is its cgroup file. A Flatpak app runs in a systemd scope named after its app id.
	Cgroup string
}

// Runs says whether p is part of e. A Flatpak matches by its app id, on its command line or its
// scope; anything else by its executable's name, which for a Proton build is a Windows path.
func (e Emulator) Runs(p Process) bool {
	if e.Kind == InstallFlatpak {
		return strings.Contains(p.Cgroup, "app-flatpak-"+e.Location+"-") || slices.Contains(p.Args, e.Location)
	}
	name := path.Base(e.Location)
	return slices.ContainsFunc(p.Args, func(a string) bool { return strings.EqualFold(lastElem(a), name) })
}

func lastElem(arg string) string {
	return arg[strings.LastIndexAny(arg, `/\`)+1:]
}

// RunningEmulator returns the emulator one of procs is part of.
func RunningEmulator(procs []Process) (Emulator, bool) {
	for _, e := range Emulators {
		if slices.ContainsFunc(procs, e.Runs) {
			return e, true
		}
	}
	return Emulator{}, false
}

// RunningESDE returns the process id of a running ES-DE: its binary, or the AppImage EmuDeck starts
// it from. The lowest id among them stands for the instance, so it stays the same until ES-DE
// restarts.
func RunningESDE(procs []Process) (int, bool) {
	pid := 0
	for _, p := range procs {
		if len(p.Args) == 0 || !isESDE(lastElem(p.Args[0])) {
			continue
		}
		if pid == 0 || p.PID < pid {
			pid = p.PID
		}
	}
	return pid, pid != 0
}

func isESDE(exe string) bool {
	exe = strings.ToLower(exe)
	switch exe {
	case "es-de", "emulationstation":
		return true
	}
	return strings.HasSuffix(exe, ".appimage") && (strings.HasPrefix(exe, "es-de") || strings.HasPrefix(exe, "emulationstation-de"))
}

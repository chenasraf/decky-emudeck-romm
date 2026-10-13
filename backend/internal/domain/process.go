package domain

import (
	"path"
	"slices"
	"strings"
)

// Process is a running process, as /proc describes it.
type Process struct {
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

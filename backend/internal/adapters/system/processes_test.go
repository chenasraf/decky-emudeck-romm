package system

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestProcesses(t *testing.T) {
	root := t.TempDir()
	write := func(pid, name, content string) {
		t.Helper()
		dir := filepath.Join(root, pid)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("100", "cmdline", "/app/bin/retroarch\x00-L\x00core.so\x00")
	write("100", "cgroup", "0::/app.slice/app-flatpak-org.libretro.RetroArch-1.scope\n")
	write("2", "cmdline", "")
	write("self", "cmdline", "ignored\x00")

	procs, err := Processes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 {
		t.Fatalf("procs: %+v", procs)
	}
	p := procs[0]
	if !slices.Equal(p.Args, []string{"/app/bin/retroarch", "-L", "core.so"}) || p.Cgroup != "0::/app.slice/app-flatpak-org.libretro.RetroArch-1.scope\n" {
		t.Fatalf("proc: %+v", p)
	}
}

// The real /proc lists at least this test.
func TestProcessesReadsProc(t *testing.T) {
	if _, err := os.Stat("/proc/self/cmdline"); err != nil {
		t.Skip("no /proc")
	}
	procs, err := Processes("/proc")
	if err != nil || len(procs) == 0 {
		t.Fatalf("procs: %d, %v", len(procs), err)
	}
}

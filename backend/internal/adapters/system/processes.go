package system

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// Processes lists the processes under procRoot, normally /proc. Kernel threads, which have no
// command line, and processes that exit while it reads are left out.
func Processes(procRoot string) ([]domain.Process, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}
	var out []domain.Process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(procRoot, e.Name())
		cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		cgroup, _ := os.ReadFile(filepath.Join(dir, "cgroup"))
		out = append(out, domain.Process{
			PID:    pid,
			Args:   strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00"),
			Cgroup: string(cgroup),
		})
	}
	return out, nil
}

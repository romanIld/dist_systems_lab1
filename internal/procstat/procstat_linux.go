//go:build linux

package procstat

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// clkTck is the kernel USER_HZ. It is 100 on essentially every Linux build; the
// stdlib exposes no sysconf, so it is assumed.
const clkTck = 100

type osProc struct {
	pid      int
	pageSize int64
}

func openOSProc(pid int) (*osProc, error) {
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err != nil {
		return nil, err
	}
	return &osProc{pid: pid, pageSize: int64(os.Getpagesize())}, nil
}

func (p *osProc) close() {}

// sample reads cumulative CPU time from /proc/<pid>/stat (utime+stime) and RSS
// from /proc/<pid>/statm (resident pages).
func (p *osProc) sample() (cpu time.Duration, rss uint64, ok bool) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", p.pid))
	if err != nil {
		return 0, 0, false
	}
	// Field 2 is "(comm)" and may contain spaces/parens; skip past the last ')'.
	s := string(stat)
	if i := strings.LastIndexByte(s, ')'); i >= 0 {
		s = s[i+1:]
	}
	f := strings.Fields(s)
	// After the ')' the first field is "state", so utime is index 11 and stime 12.
	if len(f) < 13 {
		return 0, 0, false
	}
	utime, _ := strconv.ParseInt(f[11], 10, 64)
	stime, _ := strconv.ParseInt(f[12], 10, 64)
	cpu = time.Duration((utime + stime) * int64(time.Second) / clkTck)

	if statm, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", p.pid)); err == nil {
		if parts := strings.Fields(string(statm)); len(parts) >= 2 {
			if pages, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				rss = uint64(pages * p.pageSize)
			}
		}
	}
	return cpu, rss, true
}

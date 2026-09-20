//go:build windows

package procstat

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	psapi                    = syscall.NewLazyDLL("psapi.dll")
	procOpenProcess          = kernel32.NewProc("OpenProcess")
	procCloseHandle          = kernel32.NewProc("CloseHandle")
	procGetProcessTimes      = kernel32.NewProc("GetProcessTimes")
	procGetProcessMemoryInfo = psapi.NewProc("GetProcessMemoryInfo")
)

const processQueryLimitedInformation = 0x1000

type filetime struct{ low, high uint32 }

func (f filetime) ticks() uint64 { return uint64(f.high)<<32 | uint64(f.low) }

// processMemoryCounters mirrors the Win32 PROCESS_MEMORY_COUNTERS struct.
type processMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

type osProc struct{ handle uintptr }

func openOSProc(pid int) (*osProc, error) {
	h, _, err := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return nil, fmt.Errorf("OpenProcess(%d): %v", pid, err)
	}
	return &osProc{handle: h}, nil
}

func (p *osProc) close() {
	if p.handle != 0 {
		procCloseHandle.Call(p.handle)
		p.handle = 0
	}
}

// sample returns cumulative CPU time (kernel + user) and the working set size.
func (p *osProc) sample() (cpu time.Duration, rss uint64, ok bool) {
	var creation, exit, kernel, user filetime
	r, _, _ := procGetProcessTimes.Call(p.handle,
		uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return 0, 0, false
	}
	// FILETIME counts 100-nanosecond intervals.
	cpu = time.Duration((kernel.ticks() + user.ticks()) * 100)

	var mem processMemoryCounters
	mem.cb = uint32(unsafe.Sizeof(mem))
	r, _, _ = procGetProcessMemoryInfo.Call(p.handle, uintptr(unsafe.Pointer(&mem)), uintptr(mem.cb))
	if r != 0 {
		rss = uint64(mem.workingSetSize)
	}
	return cpu, rss, true
}

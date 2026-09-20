//go:build windows

package hrtime

import (
	"syscall"
	"unsafe"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procQueryPerfCounter   = kernel32.NewProc("QueryPerformanceCounter")
	procQueryPerfFrequency = kernel32.NewProc("QueryPerformanceFrequency")

	qpcFreq int64 // performance-counter ticks per second
)

func init() {
	_, _, _ = procQueryPerfFrequency.Call(uintptr(unsafe.Pointer(&qpcFreq)))
	if qpcFreq <= 0 {
		qpcFreq = 1 // guard against division by zero on unsupported hardware
	}
}

// Now returns monotonic nanoseconds, backed by QueryPerformanceCounter.
func Now() int64 {
	var counter int64
	_, _, _ = procQueryPerfCounter.Call(uintptr(unsafe.Pointer(&counter)))
	// Ticks -> ns; split to avoid int64 overflow on long uptimes.
	sec := counter / qpcFreq
	rem := counter % qpcFreq
	return sec*1_000_000_000 + rem*1_000_000_000/qpcFreq
}

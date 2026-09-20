//go:build !windows && !linux

package procstat

import "time"

// osProc is a no-op on platforms without a supported implementation; CPU and RSS
// are reported as zero.
type osProc struct{}

func openOSProc(pid int) (*osProc, error) { return &osProc{}, nil }

func (p *osProc) close() {}

func (p *osProc) sample() (cpu time.Duration, rss uint64, ok bool) { return 0, 0, false }

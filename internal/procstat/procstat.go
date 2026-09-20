// Package procstat samples a process's CPU and memory so the benchmark can
// report per-approach resource use (req 4.4). It uses OS syscalls directly
// (GetProcessTimes / GetProcessMemoryInfo on Windows, /proc on Linux); other
// platforms report zeros.
package procstat

import (
	"context"
	"time"
)

// Result aggregates a sampling session. CPU percent is relative to one core and
// may exceed 100 on a multi-core machine.
type Result struct {
	Samples     int
	PeakRSSMB   float64
	MeanRSSMB   float64
	PeakCPUPerc float64
	MeanCPUPerc float64
}

// Sampler polls one process until stopped. CPU percent is derived from the delta
// of cumulative CPU time between two samples, so the first sample yields only an
// RSS figure.
type Sampler struct {
	proc     *osProc
	interval time.Duration

	haveLast bool
	lastCPU  time.Duration
	lastWall time.Time

	peakRSS  uint64
	sumRSS   float64
	memCount int

	peakCPU  float64
	sumCPU   float64
	cpuCount int
}

// New returns a Sampler for pid, polling every interval.
func New(pid int, interval time.Duration) (*Sampler, error) {
	p, err := openOSProc(pid)
	if err != nil {
		return nil, err
	}
	return &Sampler{proc: p, interval: interval}, nil
}

// Run samples until ctx is cancelled, then returns the aggregate. A first sample
// is taken at once (RSS only), a second after a short warm-up, so even sub-100ms
// runs yield a CPU data point.
func (s *Sampler) Run(ctx context.Context) Result {
	defer s.proc.close()

	s.sampleOnce()

	warmup := s.interval / 2
	if warmup > 30*time.Millisecond {
		warmup = 30 * time.Millisecond
	}
	select {
	case <-ctx.Done():
		return s.result()
	case <-time.After(warmup):
		s.sampleOnce()
	}

	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return s.result()
		case <-t.C:
			s.sampleOnce()
		}
	}
}

func (s *Sampler) sampleOnce() {
	cpu, rss, ok := s.proc.sample()
	if !ok {
		return
	}
	now := time.Now()

	if rss > 0 {
		s.memCount++
		s.sumRSS += float64(rss)
		if rss > s.peakRSS {
			s.peakRSS = rss
		}
	}

	if s.haveLast {
		if dWall := now.Sub(s.lastWall); dWall > 0 {
			pct := float64(cpu-s.lastCPU) / float64(dWall) * 100
			if pct < 0 {
				pct = 0
			}
			s.cpuCount++
			s.sumCPU += pct
			if pct > s.peakCPU {
				s.peakCPU = pct
			}
		}
	}
	s.haveLast = true
	s.lastCPU = cpu
	s.lastWall = now
}

func (s *Sampler) result() Result {
	const mb = 1024 * 1024
	r := Result{Samples: s.cpuCount}
	if s.memCount > 0 {
		r.PeakRSSMB = float64(s.peakRSS) / mb
		r.MeanRSSMB = s.sumRSS / float64(s.memCount) / mb
	}
	if s.cpuCount > 0 {
		r.PeakCPUPerc = s.peakCPU
		r.MeanCPUPerc = s.sumCPU / float64(s.cpuCount)
	}
	return r
}

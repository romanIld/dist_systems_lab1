// Package metrics collects per-message RTT samples and derives avg/min/max/p95
// plus throughput.
package metrics

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Collector accumulates RTT samples. Not safe for concurrent use: give each
// goroutine its own and combine them with Merge.
type Collector struct {
	samples []time.Duration
}

// NewCollector returns a Collector pre-sized for the expected number of samples.
func NewCollector(capacityHint int) *Collector {
	return &Collector{samples: make([]time.Duration, 0, capacityHint)}
}

// Add records a single round-trip time.
func (c *Collector) Add(rtt time.Duration) {
	c.samples = append(c.samples, rtt)
}

// Merge appends every sample from other into c.
func (c *Collector) Merge(other *Collector) {
	c.samples = append(c.samples, other.samples...)
}

// Count returns the number of recorded samples.
func (c *Collector) Count() int { return len(c.samples) }

// Summary is an immutable snapshot of the collected statistics.
type Summary struct {
	Count      int
	Min        time.Duration
	Max        time.Duration
	Mean       time.Duration
	P95        time.Duration
	Total      time.Duration // wall-clock duration of the whole run
	Throughput float64       // messages per second, = Count / Total
}

// Summarize reduces the samples; total is the run's wall-clock time (used for
// throughput).
func (c *Collector) Summarize(total time.Duration) Summary {
	s := Summary{Count: len(c.samples), Total: total}
	if s.Count == 0 {
		return s
	}

	sorted := slices.Clone(c.samples)
	slices.Sort(sorted)

	s.Min = sorted[0]
	s.Max = sorted[len(sorted)-1]

	var sum time.Duration
	for _, v := range sorted {
		sum += v
	}
	s.Mean = sum / time.Duration(len(sorted))
	s.P95 = percentile(sorted, 95)

	if total > 0 {
		s.Throughput = float64(s.Count) / total.Seconds()
	}
	return s
}

// percentile returns the p-th percentile (0..100) of a sorted slice by the
// nearest-rank method.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(float64(len(sorted))*p/100.0 + 0.5)
	rank = max(rank, 1)
	rank = min(rank, len(sorted))
	return sorted[rank-1]
}

// Report is the JSON view of a Summary (durations in ms) that clients print for
// the benchmark orchestrator.
type Report struct {
	Label        string  `json:"label"`
	Count        int     `json:"count"`
	MinMS        float64 `json:"min_ms"`
	MaxMS        float64 `json:"max_ms"`
	MeanMS       float64 `json:"mean_ms"`
	P95MS        float64 `json:"p95_ms"`
	TotalMS      float64 `json:"total_ms"`
	ThroughputHz float64 `json:"throughput_msg_per_s"`
}

// Report converts the summary into its JSON view, tagged with label.
func (s Summary) Report(label string) Report {
	ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	return Report{
		Label:        label,
		Count:        s.Count,
		MinMS:        ms(s.Min),
		MaxMS:        ms(s.Max),
		MeanMS:       ms(s.Mean),
		P95MS:        ms(s.P95),
		TotalMS:      ms(s.Total),
		ThroughputHz: s.Throughput,
	}
}

// JSON renders the report as a compact single-line JSON document.
func (r Report) JSON() string {
	b, _ := json.Marshal(r)
	return string(b)
}

// String renders the summary in the assignment's layout plus p95 and throughput.
func (s Summary) String() string {
	ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	return fmt.Sprintf(
		"Messages sent: %d\n"+
			"Average RTT: %.3f ms\n"+
			"Min RTT: %.3f ms\n"+
			"Max RTT: %.3f ms\n"+
			"P95 RTT: %.3f ms\n"+
			"Total time: %.2f ms\n"+
			"Throughput: %.1f msg/s",
		s.Count, ms(s.Mean), ms(s.Min), ms(s.Max), ms(s.P95), ms(s.Total), s.Throughput)
}

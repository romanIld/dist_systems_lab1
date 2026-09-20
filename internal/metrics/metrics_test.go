package metrics

import (
	"testing"
	"time"
)

func TestSummarizeBasic(t *testing.T) {
	c := NewCollector(10)
	// 1..10 ms
	for i := 1; i <= 10; i++ {
		c.Add(time.Duration(i) * time.Millisecond)
	}

	s := c.Summarize(2 * time.Second)

	if s.Count != 10 {
		t.Fatalf("Count = %d, want 10", s.Count)
	}
	if s.Min != 1*time.Millisecond {
		t.Errorf("Min = %v, want 1ms", s.Min)
	}
	if s.Max != 10*time.Millisecond {
		t.Errorf("Max = %v, want 10ms", s.Max)
	}
	if s.Mean != 5500*time.Microsecond {
		t.Errorf("Mean = %v, want 5.5ms", s.Mean)
	}
	// nearest-rank p95 of 10 samples -> rank round(9.5+0.5)=10 -> 10ms
	if s.P95 != 10*time.Millisecond {
		t.Errorf("P95 = %v, want 10ms", s.P95)
	}
	if got := s.Throughput; got != 5.0 {
		t.Errorf("Throughput = %v, want 5", got)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	c := NewCollector(0)
	s := c.Summarize(time.Second)
	if s.Count != 0 || s.Throughput != 0 {
		t.Fatalf("empty summary = %+v", s)
	}
}

func TestPercentileNearestRank(t *testing.T) {
	sorted := make([]time.Duration, 100)
	for i := 0; i < 100; i++ {
		sorted[i] = time.Duration(i+1) * time.Millisecond // 1..100 ms
	}
	if got := percentile(sorted, 95); got != 95*time.Millisecond {
		t.Errorf("p95 = %v, want 95ms", got)
	}
	if got := percentile(sorted, 100); got != 100*time.Millisecond {
		t.Errorf("p100 = %v, want 100ms", got)
	}
	if got := percentile(sorted, 0); got != 1*time.Millisecond {
		t.Errorf("p0 = %v, want 1ms", got)
	}
}

func TestMerge(t *testing.T) {
	a := NewCollector(4)
	a.Add(1 * time.Millisecond)
	a.Add(3 * time.Millisecond)

	b := NewCollector(4)
	b.Add(2 * time.Millisecond)
	b.Add(4 * time.Millisecond)

	a.Merge(b)
	if a.Count() != 4 {
		t.Fatalf("merged count = %d, want 4", a.Count())
	}
	s := a.Summarize(0)
	if s.Min != 1*time.Millisecond || s.Max != 4*time.Millisecond {
		t.Fatalf("merged summary min/max = %v/%v", s.Min, s.Max)
	}
}

// Package hrtime provides a monotonic high-resolution timestamp in nanoseconds.
//
// Go's runtime monotonic clock is only ~0.5-1 ms granular on many Windows
// systems, which rounds sub-millisecond loopback RTTs to 0. This package reads
// QueryPerformanceCounter on Windows (~100 ns) and uses the standard library
// elsewhere.
//
// Usage: t0 := hrtime.Now(); ...; rtt := time.Duration(hrtime.Now() - t0)
package hrtime

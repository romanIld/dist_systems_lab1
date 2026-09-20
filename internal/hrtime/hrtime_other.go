//go:build !windows

package hrtime

import "time"

// time.Now's monotonic reading is already nanosecond-resolution here.
var start = time.Now()

// Now returns monotonic nanoseconds since package init.
func Now() int64 {
	return int64(time.Since(start))
}

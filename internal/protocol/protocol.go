// Package protocol implements the line text protocol of the 1.1 and 1.2 echo
// servers: request is arbitrary UTF-8 ending with '\n', response is
// "ECHO: <request-without-newline> [t=<unix seconds, 6 decimals>]\n".
package protocol

import (
	"fmt"
	"strings"
	"time"
)

// Delimiter terminates every request and response frame.
const Delimiter = '\n'

// MaxLineBytes bounds a single frame; anything larger is a protocol error.
const MaxLineBytes = 64 * 1024

// FormatTimestamp renders t as Unix seconds with 6 decimals ("%.6f", req 1.5).
func FormatTimestamp(t time.Time) string {
	return fmt.Sprintf("%.6f", float64(t.UnixNano())/1e9)
}

// BuildResponse builds the echo response line (with trailing '\n'). payload must
// have no trailing delimiter; a stray CR from CRLF clients is dropped.
func BuildResponse(payload string, serverTime time.Time) string {
	payload = strings.TrimRight(payload, "\r")
	return "ECHO: " + payload + " [t=" + FormatTimestamp(serverTime) + "]\n"
}

// ParseResponse splits an echo response line into payload and timestamp string
// (clients use it to validate replies). A trailing delimiter is optional.
func ParseResponse(line string) (payload, timestamp string, err error) {
	line = strings.TrimRight(line, "\r\n")
	const prefix = "ECHO: "
	if !strings.HasPrefix(line, prefix) {
		return "", "", fmt.Errorf("protocol: missing %q prefix in %q", prefix, line)
	}
	line = line[len(prefix):]

	const marker = " [t="
	i := strings.LastIndex(line, marker)
	if i < 0 || !strings.HasSuffix(line, "]") {
		return "", "", fmt.Errorf("protocol: missing timestamp suffix in %q", line)
	}
	payload = line[:i]
	timestamp = line[i+len(marker) : len(line)-1]
	if timestamp == "" {
		return "", "", fmt.Errorf("protocol: empty timestamp in %q", line)
	}
	return payload, timestamp, nil
}

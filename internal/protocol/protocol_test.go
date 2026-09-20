package protocol

import (
	"testing"
	"time"
)

func TestFormatTimestamp(t *testing.T) {
	ts := time.Unix(1717171717, 123456000)
	if got := FormatTimestamp(ts); got != "1717171717.123456" {
		t.Fatalf("FormatTimestamp = %q, want %q", got, "1717171717.123456")
	}
}

func TestBuildResponse(t *testing.T) {
	ts := time.Unix(1000, 500000000) // 1000.500000
	got := BuildResponse("Hello 7", ts)
	want := "ECHO: Hello 7 [t=1000.500000]\n"
	if got != want {
		t.Fatalf("BuildResponse = %q, want %q", got, want)
	}
}

func TestBuildResponseStripsCarriageReturn(t *testing.T) {
	got := BuildResponse("Hello 1\r", time.Unix(0, 0))
	want := "ECHO: Hello 1 [t=0.000000]\n"
	if got != want {
		t.Fatalf("BuildResponse = %q, want %q", got, want)
	}
}

func TestParseResponseRoundTrip(t *testing.T) {
	ts := time.Unix(42, 250000000)
	line := BuildResponse("payload with spaces", ts)

	payload, timestamp, err := ParseResponse(line)
	if err != nil {
		t.Fatalf("ParseResponse returned error: %v", err)
	}
	if payload != "payload with spaces" {
		t.Fatalf("payload = %q, want %q", payload, "payload with spaces")
	}
	if timestamp != "42.250000" {
		t.Fatalf("timestamp = %q, want %q", timestamp, "42.250000")
	}
}

func TestParseResponseErrors(t *testing.T) {
	cases := []string{
		"hello world\n",          // no prefix
		"ECHO: payload\n",        // no timestamp marker
		"ECHO: payload [t=]\n",   // empty timestamp
		"ECHO: payload [t=1.0\n", // missing closing bracket
	}
	for _, c := range cases {
		if _, _, err := ParseResponse(c); err == nil {
			t.Errorf("ParseResponse(%q) = nil error, want error", c)
		}
	}
}

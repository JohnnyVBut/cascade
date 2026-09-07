package api

import (
	"strings"
	"testing"
)

func TestParseCPUModel(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "typical /proc/cpuinfo",
			input: "processor\t: 0\n" +
				"vendor_id\t: GenuineIntel\n" +
				"model name\t: Intel(R) Xeon(R) Platinum 8259CL CPU @ 2.50GHz\n" +
				"cpu MHz\t\t: 2500.000\n",
			want: "Intel(R) Xeon(R) Platinum 8259CL CPU @ 2.50GHz",
		},
		{
			name:  "no model name field (e.g. some ARM boards)",
			input: "processor\t: 0\nvendor_id\t: ARM\n",
			want:  "",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCPUModel(strings.NewReader(tc.input))
			if got != tc.want {
				t.Errorf("parseCPUModel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseOperstateUp(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"up\n", true},
		{"up", true},
		{"down\n", false},
		{"dormant\n", false},
		{"unknown\n", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := parseOperstateUp(tc.input); got != tc.want {
			t.Errorf("parseOperstateUp(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseSpeedMbps(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"1000\n", 1000},
		{"100", 100},
		{"-1\n", -1},        // kernel reports -1 when link is down
		{"0\n", -1},         // some drivers report 0 instead
		{"not-a-number", -1},
		{"", -1},
	}
	for _, tc := range cases {
		if got := parseSpeedMbps(tc.input); got != tc.want {
			t.Errorf("parseSpeedMbps(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

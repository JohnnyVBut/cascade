package version

import "testing"

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "v1.2.3", 0},
		{"v1.2.4", "v1.2.3", 1},
		{"v1.2.3", "v1.2.4", -1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.9.9", "v2.0.0", -1},
		{"v1.2.3", "1.2.3", 0},  // with/without v prefix
		{"v1.2.3-rc1", "v1.2.3", 0}, // pre-release suffix stripped → equal
		{"v1.3.0-alpha", "v1.2.9", 1},
		{"dev", "v1.0.0", -1},    // dev builds are 0.0.0
		{"v0.0.0", "dev", 0},
		{"v1.0.0", "v1.0.0", 0},
		{"v10.0.0", "v9.9.9", 1}, // double-digit major
	}
	for _, tt := range tests {
		got := compareSemver(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	tests := []struct {
		in   string
		want [3]int
	}{
		{"v1.2.3", [3]int{1, 2, 3}},
		{"1.2.3", [3]int{1, 2, 3}},
		{"v2.0.0-rc1", [3]int{2, 0, 0}},
		{"dev", [3]int{0, 0, 0}},
		{"", [3]int{0, 0, 0}},
		{"v10.20.30", [3]int{10, 20, 30}},
	}
	for _, tt := range tests {
		got := parseSemver(tt.in)
		if got != tt.want {
			t.Errorf("parseSemver(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

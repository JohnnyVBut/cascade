// Tests for mapToAWG2 — JSON body → peer.AWG2Settings conversion, including
// the AWG 3.0 Transport Protection fields added alongside jc/jmin/jmax/etc.
package api

import (
	"testing"

	"github.com/JohnnyVBut/cascade/internal/peer"
)

func TestMapToAWG2_AWG3FieldsExtracted(t *testing.T) {
	body := map[string]any{
		"jc":                     float64(5),
		"s3":                     float64(12),
		"s4":                     float64(12),
		"headerProtectionKey":    "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		"contentPaddingAddition": "0-1",
		"rekeyAfterTime":         "1h-2h",
		"rekeyTimeout":           "5-10",
		"rejectAfterTime":        "3h",
		"keepaliveTimeout":       "15-25",
		"maxHandshakeAttempts":   "20",
	}

	a, err := mapToAWG2(body)
	if err != nil {
		t.Fatalf("mapToAWG2: %v", err)
	}
	if a.HeaderProtectionKey != "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=" {
		t.Errorf("HeaderProtectionKey = %q, want AWG3 test key", a.HeaderProtectionKey)
	}
	if a.ContentPaddingAddition != "0-1" {
		t.Errorf("ContentPaddingAddition = %q, want '0-1'", a.ContentPaddingAddition)
	}
	if a.RekeyAfterTime != "1h-2h" {
		t.Errorf("RekeyAfterTime = %q, want '1h-2h'", a.RekeyAfterTime)
	}
	if a.RekeyTimeout != "5-10" {
		t.Errorf("RekeyTimeout = %q, want '5-10'", a.RekeyTimeout)
	}
	if a.RejectAfterTime != "3h" {
		t.Errorf("RejectAfterTime = %q, want '3h'", a.RejectAfterTime)
	}
	if a.KeepaliveTimeout != "15-25" {
		t.Errorf("KeepaliveTimeout = %q, want '15-25'", a.KeepaliveTimeout)
	}
	if a.MaxHandshakeAttempts != "20" {
		t.Errorf("MaxHandshakeAttempts = %q, want '20'", a.MaxHandshakeAttempts)
	}
}

func TestMapToAWG2_AWG3FieldsMissing_DefaultToEmpty(t *testing.T) {
	a, err := mapToAWG2(map[string]any{"jc": float64(5)})
	if err != nil {
		t.Fatalf("mapToAWG2: %v", err)
	}
	for name, v := range map[string]string{
		"HeaderProtectionKey":    a.HeaderProtectionKey,
		"ContentPaddingAddition": a.ContentPaddingAddition,
		"RekeyAfterTime":         a.RekeyAfterTime,
		"RekeyTimeout":           a.RekeyTimeout,
		"RejectAfterTime":        a.RejectAfterTime,
		"KeepaliveTimeout":       a.KeepaliveTimeout,
		"MaxHandshakeAttempts":   a.MaxHandshakeAttempts,
	} {
		if v != "" {
			t.Errorf("%s = %q, want empty when field absent from body", name, v)
		}
	}
}

// awg2FieldByKey maps a JSON body key name to the corresponding
// AWG2Settings field value, for use in table-driven tests below.
func awg2FieldByKey(a *peer.AWG2Settings, key string) string {
	switch key {
	case "headerProtectionKey":
		return a.HeaderProtectionKey
	case "contentPaddingAddition":
		return a.ContentPaddingAddition
	case "rekeyAfterTime":
		return a.RekeyAfterTime
	case "rekeyTimeout":
		return a.RekeyTimeout
	case "rejectAfterTime":
		return a.RejectAfterTime
	case "keepaliveTimeout":
		return a.KeepaliveTimeout
	case "maxHandshakeAttempts":
		return a.MaxHandshakeAttempts
	default:
		return "<unknown key>"
	}
}

// TestMapToAWG2_AWG3Fields_RejectNewlineInjection confirms the new fields get
// the same PostUp/PostDown injection protection as h1-i5 (strField rejects
// \n and \r) — worth confirming explicitly rather than assuming reuse is bug-free.
func TestMapToAWG2_AWG3Fields_RejectNewlineInjection(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"headerProtectionKey", "abc\nPostUp = malicious"},
		{"contentPaddingAddition", "abc\rmalicious"},
		{"rekeyAfterTime", "1h\nPostDown = evil"},
		{"rekeyTimeout", "5\r\nmalicious"},
		{"rejectAfterTime", "3h\nmalicious"},
		{"keepaliveTimeout", "15\rmalicious"},
		{"maxHandshakeAttempts", "20\nmalicious"},
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			a, err := mapToAWG2(map[string]any{tc.key: tc.value})
			if err != nil {
				t.Fatalf("mapToAWG2: %v", err)
			}
			got := awg2FieldByKey(a, tc.key)
			if got != "" {
				t.Errorf("%s with embedded newline should be rejected (empty string), got %q", tc.key, got)
			}
		})
	}
}

package routing

import (
	"testing"
)

// ---- validateRouteFields ----

func TestValidateRouteFields_Valid(t *testing.T) {
	cases := []struct {
		name  string
		route Route
	}{
		{
			name:  "CIDR destination with gateway and named table",
			route: Route{Destination: "10.0.0.0/8", Gateway: "192.168.1.1", Table: "main"},
		},
		{
			name:  "literal default destination with dev only",
			route: Route{Destination: "default", Dev: "wg10"},
		},
		{
			name:  "zero route with dev and numeric table",
			route: Route{Destination: "0.0.0.0/0", Dev: "eth0", Table: "100"},
		},
		{
			name:  "IPv6 CIDR with gateway",
			route: Route{Destination: "2001:db8::/32", Gateway: "fe80::1"},
		},
		{
			name:  "host route /32 with gateway and underscore table name",
			route: Route{Destination: "192.168.1.5/32", Gateway: "10.0.0.1", Table: "vpn_kz"},
		},
		{
			name:  "all fields empty except destination=default",
			route: Route{Destination: "default"},
		},
		{
			name:  "hyphen in interface name",
			route: Route{Destination: "10.1.0.0/16", Dev: "bond-1"},
		},
		{
			name:  "hyphen in table name",
			route: Route{Destination: "10.2.0.0/16", Gateway: "10.0.0.1", Table: "vpn-eu"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateRouteFields(tc.route); err != nil {
				t.Errorf("validateRouteFields(%+v) returned unexpected error: %v", tc.route, err)
			}
		})
	}
}

func TestValidateRouteFields_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		route Route
	}{
		{
			name:  "shell injection in Destination",
			route: Route{Destination: "10.0.0.0/8; id", Gateway: "192.168.1.1"},
		},
		{
			name:  "shell injection with rm in Gateway",
			route: Route{Destination: "10.0.0.0/8", Gateway: "192.168.1.1; rm -rf /"},
		},
		{
			name:  "shell injection semicolon in Dev",
			route: Route{Destination: "10.0.0.0/8", Dev: "eth0; id"},
		},
		{
			name:  "shell injection semicolon in Table",
			route: Route{Destination: "10.0.0.0/8", Gateway: "192.168.1.1", Table: "main; id"},
		},
		{
			name:  "bare IP without prefix length in Destination",
			route: Route{Destination: "10.0.0.1", Gateway: "192.168.1.1"},
		},
		{
			name:  "octet out of range in Destination CIDR",
			route: Route{Destination: "300.0.0.0/8", Gateway: "192.168.1.1"},
		},
		{
			name:  "octet out of range in Gateway",
			route: Route{Destination: "10.0.0.0/8", Gateway: "999.0.0.1"},
		},
		{
			name:  "CIDR notation in Gateway (not a plain IP)",
			route: Route{Destination: "10.0.0.0/8", Gateway: "192.168.1.1/24"},
		},
		{
			name:  "space in Dev",
			route: Route{Destination: "10.0.0.0/8", Dev: "eth 0"},
		},
		{
			name:  "Dev longer than 15 chars",
			route: Route{Destination: "10.0.0.0/8", Dev: "abcdefghijklmnop"},
		},
		{
			name:  "dot in Table",
			route: Route{Destination: "10.0.0.0/8", Gateway: "10.0.0.1", Table: "table.name"},
		},
		{
			name:  "Table longer than 31 chars",
			route: Route{Destination: "10.0.0.0/8", Gateway: "10.0.0.1", Table: "abcdefghijklmnopqrstuvwxyz012345"},
		},
		{
			name:  "backtick injection in Gateway",
			route: Route{Destination: "10.0.0.0/8", Gateway: "`whoami`"},
		},
		{
			name:  "dollar sign injection in Dev",
			route: Route{Destination: "10.0.0.0/8", Dev: "eth$0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateRouteFields(tc.route); err == nil {
				t.Errorf("validateRouteFields(%+v) expected error, got nil", tc.route)
			}
		})
	}
}

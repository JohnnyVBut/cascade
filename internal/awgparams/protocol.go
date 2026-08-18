package awgparams

// Protocol string constants stored in interfaces.protocol / used across the
// tunnel/peer/api packages. Centralized here (rather than duplicated as
// string literals) so a future protocol version bump is a small, safe change
// instead of finding every exact-match comparison by hand.
const (
	ProtocolWireGuard1 = "wireguard-1.0"
	ProtocolAmneziaWG2 = "amneziawg-2.0"
	ProtocolAmneziaWG3 = "amneziawg-3.0"
)

// IsAmneziaWG reports whether protocol is any AmneziaWG obfuscation line
// (2.0 or 3.0). Both use the same AWG2Settings-shaped params (Jc/Jmin/Jmax/
// S1-S4/H1-H4/I1-I5) for config generation; 3.0 additionally allows the
// optional Transport Protection fields to be set (enforced separately, not
// by this check — see internal/settings.CreateTemplate/UpdateTemplate).
func IsAmneziaWG(protocol string) bool {
	return protocol == ProtocolAmneziaWG2 || protocol == ProtocolAmneziaWG3
}

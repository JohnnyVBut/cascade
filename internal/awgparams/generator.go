// Package awgparams generates AWG 2.0 obfuscation parameters.
// Direct port of AwgParamGenerator.js (JohnnyVBut/AmneziaWG-Architect).
// Browser Fingerprint (BFP) ported from Vadim-Khristenko/AmneziaWG-Architect.
//
// Supported CPS profiles for I1 packet:
//
//	quic_initial     — QUIC Initial (RFC 9000, Long Header 0xC0-0xC3)
//	quic_0rtt        — QUIC 0-RTT (Long Header 0xD0-0xD3)
//	tls_client_hello — TLS 1.3 ClientHello
//	dtls             — DTLS 1.2 ClientHello
//	http3            — HTTP/3 over QUIC
//	sip              — SIP REGISTER request (no BFP entry — always uses protocol defaults)
//	wireguard_noise  — WireGuard Noise_IK handshake initiation
//	tls_to_quic      — composite: TLS ClientHello → QUIC Initial
//	quic_burst       — composite: QUIC Initial → QUIC 0-RTT → HTTP/3
//	random           — pick one of the non-composite profiles at random
package awgparams

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math/rand/v2"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// Options controls the generator behaviour.
type Options struct {
	Profile   string // CPS profile (default: "random")
	Intensity string // "low" | "medium" | "high" (default: "medium")
	Host      string // custom SNI host (empty = pick from pool)
	Browser   string // BFP browser: "chrome" | "firefox" | "safari" | "edge" | "yandex_desktop" | "yandex_mobile" | "" (= no BFP)
	IterCount int    // retry counter — increases intensity (0 = first attempt)
	Jc        int    // base Jc value 0-10 (default: 6)
}

// Params is the complete set of AWG 2.0 obfuscation parameters.
type Params struct {
	Jc      int    `json:"jc"`
	Jmin    int    `json:"jmin"`
	Jmax    int    `json:"jmax"`
	S1      int    `json:"s1"`
	S2      int    `json:"s2"`
	S3      int    `json:"s3"`
	S4      int    `json:"s4"`
	H1      string `json:"h1"`
	H2      string `json:"h2"`
	H3      string `json:"h3"`
	H4      string `json:"h4"`
	I1      string `json:"i1"`
	I2      string `json:"i2"`
	I3      string `json:"i3"`
	I4      string `json:"i4"`
	I5      string `json:"i5"`
	Profile string `json:"profile"` // resolved profile (never "random")
}

// Profile is a UI-facing profile descriptor.
type Profile struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Profiles is the list of supported profiles returned to the UI.
var Profiles = []Profile{
	{ID: "random", Label: "Random"},
	{ID: "quic_initial", Label: "QUIC Initial"},
	{ID: "quic_0rtt", Label: "QUIC 0-RTT"},
	{ID: "tls_client_hello", Label: "TLS 1.3"},
	{ID: "dtls", Label: "DTLS 1.2"},
	{ID: "http3", Label: "HTTP/3"},
	{ID: "sip", Label: "SIP"},
	{ID: "wireguard_noise", Label: "Noise_IK (WireGuard)"},
	{ID: "tls_to_quic", Label: "TLS→QUIC (composite)"},
	{ID: "quic_burst", Label: "QUIC Burst (composite)"},
}

// ── Browser Fingerprint (BFP) ─────────────────────────────────────────────────
// Ported from Vadim-Khristenko/AmneziaWG-Architect.
// Each entry defines [min, max] byte ranges for the <rc N> field per protocol.
// SIP has no BFP entry — it always uses protocol defaults.

// BfpEntry is a [min, max] inclusive byte range for <rc N>.
type BfpEntry [2]int

// BfpTable holds packet size ranges for one browser across all protocols.
type BfpTable struct {
	QI   BfpEntry // QUIC Initial
	Q0   BfpEntry // QUIC 0-RTT
	H3   BfpEntry // HTTP/3
	TLS  BfpEntry // TLS 1.3 ClientHello
	NX   BfpEntry // WireGuard Noise_IK
	DTLS BfpEntry // DTLS 1.2
}

// BFP maps browser name → packet size ranges per protocol.
var BFP = map[string]BfpTable{
	"chrome": {
		QI:   BfpEntry{1250, 1250},
		Q0:   BfpEntry{1250, 1350},
		H3:   BfpEntry{1250, 1350},
		TLS:  BfpEntry{512, 800},
		NX:   BfpEntry{1200, 1250},
		DTLS: BfpEntry{1100, 1200},
	},
	"firefox": {
		QI:   BfpEntry{1200, 1252},
		Q0:   BfpEntry{1200, 1300},
		H3:   BfpEntry{1200, 1350},
		TLS:  BfpEntry{512, 700},
		NX:   BfpEntry{1200, 1250},
		DTLS: BfpEntry{1050, 1200},
	},
	"safari": {
		QI:   BfpEntry{1250, 1252},
		Q0:   BfpEntry{1250, 1300},
		H3:   BfpEntry{1250, 1350},
		TLS:  BfpEntry{512, 750},
		NX:   BfpEntry{1200, 1250},
		DTLS: BfpEntry{1100, 1200},
	},
	"edge": {
		QI:   BfpEntry{1250, 1250},
		Q0:   BfpEntry{1250, 1350},
		H3:   BfpEntry{1250, 1350},
		TLS:  BfpEntry{512, 800},
		NX:   BfpEntry{1200, 1250},
		DTLS: BfpEntry{1100, 1200},
	},
	"yandex_desktop": {
		QI:   BfpEntry{1250, 1250},
		Q0:   BfpEntry{1250, 1350},
		H3:   BfpEntry{1350, 1350},
		TLS:  BfpEntry{512, 800},
		NX:   BfpEntry{1200, 1250},
		DTLS: BfpEntry{1100, 1200},
	},
	"yandex_mobile": {
		QI:   BfpEntry{1232, 1232},
		Q0:   BfpEntry{1250, 1350},
		H3:   BfpEntry{1350, 1350},
		TLS:  BfpEntry{512, 800},
		NX:   BfpEntry{1200, 1250},
		DTLS: BfpEntry{1100, 1200},
	},
}

// bfpRc returns a random value within the BFP range for (browser, key),
// or fallback if browser is empty / unknown, or key is not applicable.
// key is one of: "qi", "q0", "h3", "tls", "nx", "dtls".
func bfpRc(browser, key string, fallback int) int {
	tbl, ok := BFP[browser]
	if !ok {
		return fallback
	}
	var e BfpEntry
	switch key {
	case "qi":
		e = tbl.QI
	case "q0":
		e = tbl.Q0
	case "h3":
		e = tbl.H3
	case "tls":
		e = tbl.TLS
	case "nx":
		e = tbl.NX
	case "dtls":
		e = tbl.DTLS
	default:
		return fallback
	}
	return rnd(e[0], e[1])
}

// ── Host pools ────────────────────────────────────────────────────────────────

var hostPools = map[string][]string{
	"quic_initial": {
		"yandex.net", "yastatic.net", "storage.yandexcloud.net", "cloud.yandex.ru",
		"vk.com", "mycdn.me", "vk-cdn.net", "ok.ru", "mail.ru", "avito.ru",
		"ozon.ru", "wildberries.ru", "kinopoisk.ru", "sber.ru", "tbank.ru",
		"github.com", "objects.githubusercontent.com", "cdn.jsdelivr.net",
		"steamstatic.com", "steamcontent.com", "wikipedia.org",
		"gcore.com", "bunny.net", "fastly.net", "a248.e.akamai.net",
		"cloudfront.net", "microsoft.com", "icloud.com", "apple.com",
		"hetzner.com", "ovhcloud.com", "tencentcs.com", "alicdn.com",
	},
	"quic_0rtt": {
		"yandex.net", "yastatic.net", "vk.com", "ok.ru", "mail.ru",
		"avito.ru", "ozon.ru", "wildberries.ru", "sber.ru", "tbank.ru",
		"github.com", "microsoft.com", "apple.com", "icloud.com",
		"gcore.com", "fastly.net", "akamaiedge.net", "cloudfront.net",
	},
	"tls_client_hello": {
		"yandex.ru", "yandex.net", "yastatic.net", "vk.com", "ok.ru",
		"mail.ru", "avito.ru", "ozon.ru", "wildberries.ru", "kinopoisk.ru",
		"sber.ru", "sberbank.ru", "tbank.ru", "vtb.ru", "alfabank.ru",
		"github.com", "gitlab.com", "microsoft.com", "office.com",
		"apple.com", "icloud.com", "steamcontent.com", "wikipedia.org",
		"gcore.com", "bunny.net", "fastly.net", "akamaiedge.net",
		"cloudfront.net", "hetzner.com", "ovhcloud.com",
	},
	"dtls": {
		"stun.yandex.net", "stun1.l.google.com", "stun2.l.google.com",
		"stun.cloudflare.com", "stun.nextcloud.com", "stun.sipnet.ru",
		"stun.services.mozilla.com", "stun.voip.eutelia.it",
		"stun.ekiga.net", "stunserver.stunprotocol.org",
		"stun.1und1.de", "stun.t-online.de", "stun.hetzner.de",
		"global.stun.twilio.com", "stun.sip.us", "stun.counterpath.net",
	},
	"sip": {
		"sip.beeline.ru", "sip.megafon.ru", "sip.mts.ru",
		"sipnet.ru", "sip.zadarma.com", "sip.onlinepbx.ru",
		"sip2.zadarma.com", "registrar.sip.net", "sip.bicom.com",
		"sip.antisip.com", "proxy01.sipphone.com",
	},
}

// ── Public API ────────────────────────────────────────────────────────────────

// Generate produces a complete set of AWG 2.0 parameters.
// Mirrors AwgParamGenerator.generate() from Node.js exactly.
func Generate(opts Options) Params {
	// Defaults
	if opts.Profile == "" {
		opts.Profile = "random"
	}
	if opts.Intensity == "" {
		opts.Intensity = "medium"
	}
	if opts.Jc == 0 {
		opts.Jc = 6
	}

	imap := map[string]int{"low": 1, "medium": 2, "high": 3}
	iv := imap[opts.Intensity]
	if iv == 0 {
		iv = 2
	}
	if opts.IterCount > 3 {
		iv++
	}
	boost := opts.IterCount * 5

	// H1-H4 — диапазоны в 4 непересекающихся зонах uint32 (как в AwgParamGenerator.js)
	h1 := rRange(100_000_000)
	h2 := rRange(1_200_000_000)
	h3 := rRange(2_400_000_000)
	h4 := rRange(3_600_000_000)

	// S1-S4 — размеры пакетов
	s1 := min(64, rnd(15, 32)+boost)
	s2 := min(64, rnd(15, 32)+boost)
	if s2 == s1+56 { // критичное ограничение: S1+56 ≠ S2
		s2++
	}
	s3 := min(64, rnd(8, 24)+boost)
	s4 := min(32, rnd(6, 18)+boost)

	// Jc / Jmin / Jmax
	jcExtra := 0
	if opts.Intensity == "high" {
		jcExtra = 2
	}
	jc := max(3, min(10, opts.Jc+jcExtra))
	jmin := 64 + boost*2
	jmax := min(1280, 256+iv*150+boost*10)

	// Resolve profile (random → конкретный, composite excluded from random pool)
	resolvedProfile := opts.Profile
	if resolvedProfile == "random" {
		profiles := []string{
			"quic_initial", "quic_0rtt", "tls_client_hello",
			"dtls", "http3", "sip", "wireguard_noise",
		}
		resolvedProfile = profiles[rnd(0, len(profiles)-1)]
	}

	i1 := genI1(resolvedProfile, iv, opts.Host, opts.Browser)
	i2 := mkEntropy(1, iv)
	i3 := mkEntropy(2, iv)
	i4 := mkEntropy(3, iv)
	i5 := mkEntropy(4, iv)

	return Params{
		Jc: jc, Jmin: jmin, Jmax: jmax,
		S1: s1, S2: s2, S3: s3, S4: s4,
		H1: h1, H2: h2, H3: h3, H4: h4,
		I1: i1, I2: i2, I3: i3, I4: i4, I5: i5,
		Profile: resolvedProfile,
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// rnd returns a random int in [a, b] inclusive. Mirrors JS rnd(a, b).
func rnd(a, b int) int {
	if b <= a {
		return a
	}
	return a + rand.IntN(b-a+1)
}

// rh returns n random bytes encoded as a hex string (always even length).
// Uses crypto/rand for quality entropy. Mirrors JS rh(n).
func rh(n int) string {
	n = max(0, n)
	if n == 0 {
		return ""
	}
	b := make([]byte, n)
	_, _ = cryptorand.Read(b)
	return hex.EncodeToString(b)
}

// hexPad formats value as hex padded to byteLen bytes (byteLen*2 hex chars).
// Mirrors JS hexPad(value, byteLen).
func hexPad(value, byteLen int) string {
	h := fmt.Sprintf("%x", value)
	for len(h) < byteLen*2 {
		h = "0" + h
	}
	if len(h) > byteLen*2 {
		h = h[len(h)-byteLen*2:]
	}
	return h
}

// assertEvenHex ensures hex string has even length (required for valid hex).
// Mirrors JS assertEvenHex(hex, label).
func assertEvenHex(h, label string) string {
	if len(h)%2 != 0 {
		log.Printf("[awgparams] odd hex in %s len=%d — padding", label, len(h))
		h = h + "0"
	}
	return h
}

// rRange generates an H-range string "start-end" with base offset.
// Mirrors JS rRange(base).
func rRange(base int) string {
	s := base + rnd(0, 500_000)
	return fmt.Sprintf("%d-%d", s, s+rnd(1_000, 50_000))
}

// getHost picks a random host from the pool for the given profile,
// or returns customHost if provided. Mirrors JS getHost(pool, customHost).
func getHost(pool, customHost string) string {
	if customHost != "" {
		return customHost
	}
	hosts := hostPools[pool]
	if len(hosts) == 0 {
		hosts = hostPools["tls_client_hello"]
	}
	return hosts[rnd(0, len(hosts)-1)]
}

// ── CPS protocol packet generators ───────────────────────────────────────────

// mkQUICi generates a QUIC Initial packet (Long Header 0xC0-0xC3).
// Mirrors JS mkQUICi(iv, host). BFP key: "qi".
func mkQUICi(iv int, host, browser string) string {
	dcid := rnd(8, 20)
	scid := rnd(0, 20)
	tokenLen := 0
	if rnd(0, 1) == 1 {
		tokenLen = rnd(8, 32)
	}
	sniRc := min(len(host)+rnd(0, 6), 64)
	rLen := min(rnd(20, 80)*iv, 500)

	h := assertEvenHex(
		hexPad(0xc0|rnd(0, 3), 1)+
			"00000001"+
			hexPad(dcid, 1)+rh(dcid)+
			hexPad(scid, 1)+rh(scid)+
			hexPad(tokenLen, 1)+rh(tokenLen)+
			rh(4),
		"mkQUICi",
	)
	return fmt.Sprintf("<b 0x%s><rc %d><t><r %d>", h, bfpRc(browser, "qi", sniRc), rLen)
}

// mkQUIC0 generates a QUIC 0-RTT packet (Long Header 0xD0-0xD3).
// Mirrors JS mkQUIC0(iv, host). BFP key: "q0".
func mkQUIC0(iv int, host, browser string) string {
	dcid := rnd(8, 20)
	scid := rnd(0, 20)
	ticketHint := min(len(host)+rnd(4, 16), 48)
	rLen := min(rnd(30, 120)*iv, 600)

	h := assertEvenHex(
		hexPad(0xd0|rnd(0, 3), 1)+
			"00000001"+
			hexPad(dcid, 1)+rh(dcid)+
			hexPad(scid, 1)+rh(scid)+
			rh(4),
		"mkQUIC0",
	)
	return fmt.Sprintf("<b 0x%s><t><r %d><rc %d>", h, rLen, bfpRc(browser, "q0", ticketHint))
}

// mkTLS generates a TLS 1.3 ClientHello record header.
// Mirrors JS mkTLS(iv, host). BFP key: "tls".
func mkTLS(iv int, host, browser string) string {
	recLen := rnd(300, 550)
	hsLen := recLen - rnd(4, 9)
	sniExt := 2 + 2 + 2 + 1 + 2 + len(host)
	sniRc := min(sniExt, 64)
	rLen := min(rnd(20, 60)*iv, 300)

	h := assertEvenHex(
		"160301"+
			hexPad(recLen, 2)+
			"01"+
			hexPad(hsLen, 3)+
			"0303"+
			rh(32),
		"mkTLS",
	)
	return fmt.Sprintf("<b 0x%s><rc %d><r %d><t>", h, bfpRc(browser, "tls", sniRc), rLen)
}

// mkNoise generates a WireGuard Noise_IK Handshake Initiation packet.
// Mirrors JS mkNoise(iv). BFP key: "nx".
func mkNoise(iv int, browser string) string {
	rLen := min(rnd(10, 40)*iv, 200)
	rcLen := rnd(4, 12)
	return fmt.Sprintf(
		"<b 0x01000000%s><b 0x%s><b 0x%s><b 0x%s><r %d><t><rc %d>",
		rh(4), rh(32), rh(48), rh(28), rLen, bfpRc(browser, "nx", rcLen),
	)
}

// mkDTLS generates a DTLS 1.2 ClientHello record.
// Mirrors JS mkDTLS(iv, host). BFP key: "dtls".
func mkDTLS(iv int, host, browser string) string {
	fragLen := rnd(100, 300)
	sniRc := min(len(host)+rnd(2, 8), 60)
	epoch := rnd(0, 255)
	rLen := min(rnd(15, 50)*iv, 250)

	h := assertEvenHex(
		"16"+
			"fefd"+
			hexPad(epoch, 2)+
			rh(6)+
			hexPad(fragLen, 2)+
			"01"+
			rh(6)+
			"fefd0000"+
			rh(4)+
			rh(32),
		"mkDTLS",
	)
	return fmt.Sprintf("<b 0x%s><rc %d><t><r %d>", h, bfpRc(browser, "dtls", sniRc), rLen)
}

// mkHTTP3 generates an HTTP/3 over QUIC packet.
// Mirrors JS mkHTTP3(iv, host). BFP key: "h3".
func mkHTTP3(iv int, host, browser string) string {
	ptypes := []int{0xc0, 0xc1, 0xc2, 0xc3, 0xe0, 0xe1, 0xe2}
	dcid := rnd(8, 20)
	scid := rnd(0, 20)
	sniLen := min(len(host)+9+rnd(0, 6), 64)
	rLen := min(rnd(30, 100)*iv, 500)

	h := assertEvenHex(
		hexPad(ptypes[rnd(0, len(ptypes)-1)], 1)+
			"00000001"+
			hexPad(dcid, 1)+rh(dcid)+
			hexPad(scid, 1)+rh(scid)+
			rh(4),
		"mkHTTP3",
	)
	return fmt.Sprintf("<b 0x%s><rc %d><r %d><t>", h, bfpRc(browser, "h3", sniLen), rLen)
}

// mkSIP generates a SIP REGISTER request packet (ASCII bytes as hex).
// Mirrors JS mkSIP(iv, host). No BFP entry — always uses protocol defaults.
func mkSIP(iv int, host string) string {
	// "REGISTER sip:" = 13 bytes — always even hex (26 chars)
	hostHex := hex.EncodeToString([]byte(host))
	h := assertEvenHex(
		"524547495354455220736970"+ // "REGISTER sip"
			"3a"+ // ":"
			hostHex+
			"20"+ // " "
			rh(4),
		"mkSIP",
	)
	rcVal := min(len(host)+rnd(8, 24)*iv, 150)
	rLen := min(rnd(5, 30)*iv, 120)
	return fmt.Sprintf("<b 0x%s><rc %d><t><r %d>", h, rcVal, rLen)
}

// mkTLStoQUIC generates a composite TLS→QUIC packet (TLS ClientHello then QUIC Initial).
func mkTLStoQUIC(iv int, host, browser string) string {
	tls := mkTLS(iv, getHost("tls_client_hello", host), browser)
	qi := mkQUICi(iv, getHost("quic_initial", host), browser)
	return tls + qi
}

// mkQUICBurst generates a composite QUIC burst packet (Initial → 0-RTT → HTTP/3).
func mkQUICBurst(iv int, host, browser string) string {
	qi := mkQUICi(iv, getHost("quic_initial", host), browser)
	q0 := mkQUIC0(iv, getHost("quic_0rtt", host), browser)
	h3 := mkHTTP3(iv, getHost("quic_initial", host), browser)
	return qi + q0 + h3
}

// mkEntropy generates an entropy packet for I2-I5.
// <c> (counter tag) is excluded — causes issues with some AWG clients.
// Mirrors JS mkEntropy(idx, iv).
func mkEntropy(idx, iv int) string {
	rLen := min(rnd(10, 40)*iv, 300)
	rcLen := rnd(4, 12)
	rdLen := rnd(4, 8)

	b := ""
	if iv >= 2 {
		b = fmt.Sprintf("<b 0x%s>", rh(rnd(4, 8*iv)))
	}
	r := fmt.Sprintf("<r %d>", rLen)
	t := "<t>"
	rc := fmt.Sprintf("<rc %d>", rcLen)
	rd := fmt.Sprintf("<rd %d>", rdLen)

	patterns := []string{
		b + r + t + rc + rd,
		t + b + r + rc + rd,
		rc + b + r + t + rd,
		t + r + rc + b + rd,
		r + rc + b + t + rd,
	}

	res := patterns[(idx+rnd(0, 4))%len(patterns)]
	if res == "" {
		return "<r 10>"
	}
	return res
}

// genI1 dispatches to the correct I1 generator by profile.
// Mirrors JS genI1(profile, iv, host).
func genI1(profile string, iv int, host, browser string) string {
	switch profile {
	case "quic_initial":
		return mkQUICi(iv, getHost("quic_initial", host), browser)
	case "quic_0rtt":
		return mkQUIC0(iv, getHost("quic_0rtt", host), browser)
	case "tls_client_hello":
		return mkTLS(iv, getHost("tls_client_hello", host), browser)
	case "wireguard_noise":
		return mkNoise(iv, browser)
	case "dtls":
		return mkDTLS(iv, getHost("dtls", host), browser)
	case "http3":
		return mkHTTP3(iv, getHost("quic_initial", host), browser)
	case "sip":
		return mkSIP(iv, getHost("sip", host))
	case "tls_to_quic":
		return mkTLStoQUIC(iv, host, browser)
	case "quic_burst":
		return mkQUICBurst(iv, host, browser)
	default:
		return mkQUICi(iv, getHost("quic_initial", host), browser)
	}
}

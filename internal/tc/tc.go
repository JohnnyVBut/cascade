// Package tc manages Linux traffic control (tc) rules for per-client bandwidth limiting.
//
// Architecture:
//   - Egress  (download ↓, server→client): tc HTB root qdisc on the WireGuard interface.
//     Each limited peer gets an HTB class; unlimited peers use the default class (1:999, 1gbit).
//   - Ingress (upload ↑, client→server):   tc ingress qdisc + police filter per peer IP.
//
// Classid is derived deterministically from the peer's VPN IP address:
//   last two octets of IP → classid integer (e.g. 10.8.0.5 → 5, 10.8.1.5 → 261).
//   Special case: classid 999 is reserved for the HTB default class → remapped to 1000.
//
// All tc errors are non-fatal: logged but never propagated to callers.
// Rules vanish automatically when the WireGuard interface is brought down (wg-quick down).
// They are restored by calling RestoreAll after each interface Start().
package tc

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/JohnnyVBut/cascade/internal/util"
)

const tcTimeout = 5 * time.Second

const (
	defaultClassID = 999  // HTB default class — unlimited traffic
	defaultRate    = "1gbit"
)

// ClassIDFromIP derives a tc classid from a peer's VPN IP address.
// Uses the last two octets: 10.8.0.5 → 5, 10.8.1.5 → 261.
// Returns 0 if the IP cannot be parsed.
func ClassIDFromIP(ipCIDR string) int {
	host := strings.SplitN(ipCIDR, "/", 2)[0]
	ip := net.ParseIP(host)
	if ip == nil {
		return 0
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return 0
	}
	classID := int(ip4[2])*256 + int(ip4[3])
	if classID == 0 {
		return 0
	}
	if classID == defaultClassID {
		classID = 1000 // avoid collision with default class
	}
	return classID
}

// Standard WireGuard MTU values used for overhead compensation.
// tc counts outer (encrypted) packet bytes; the user-visible throughput is
// based on inner (payload) bytes. To achieve targetKbps of useful throughput,
// the tc rate must be set higher by the ratio outerMTU/wgMTU.
const (
	outerMTU = 1500 // standard Ethernet MTU
	wgMTU    = 1420 // default WireGuard inner MTU (1500 - 80 bytes overhead)
)

// tcKbps converts a user-specified kbps target into the tc rate needed to
// achieve that throughput at the application level, compensating for
// WireGuard packet overhead.
// ifaceMTU is the actual WireGuard inner MTU; 0 means use the default (1420).
//   targetKbps=10000, ifaceMTU=1420 → tc rate ≈ 10563 kbps → user sees ~10.0 Mbit/s
//   targetKbps=10000, ifaceMTU=1280 → tc rate ≈ 11719 kbps → user sees ~10.0 Mbit/s
func tcKbps(targetKbps, ifaceMTU int) int {
	inner := ifaceMTU
	if inner <= 0 {
		inner = wgMTU
	}
	return targetKbps * outerMTU / inner
}

// burstBytes calculates an appropriate tc burst size for a given tc rate in kbps.
// burst ≈ 100 ms worth of data, minimum 1500 bytes (one Ethernet frame).
func burstBytes(kbps int) int {
	// 100 ms of data at kbps: kbps * 1000 bits/s * 0.1 s / 8 bits/byte = kbps * 12.5
	b := kbps * 1000 / 80
	if b < 1500 {
		b = 1500
	}
	return b
}

// EnsureQdisc ensures the root HTB qdisc and ingress qdisc exist on the interface.
// Safe to call multiple times — checks before adding.
func EnsureQdisc(ifaceID string) {
	out, err := util.Exec(fmt.Sprintf("tc qdisc show dev %s", ifaceID), tcTimeout, false)
	if err != nil {
		log.Printf("tc: qdisc show %s: %v", ifaceID, err)
		return
	}

	// Root HTB qdisc (egress).
	if !strings.Contains(out, "htb") {
		if _, err := util.Exec(fmt.Sprintf(
			"tc qdisc add dev %s root handle 1: htb default %x",
			ifaceID, defaultClassID,
		), tcTimeout, false); err != nil {
			log.Printf("tc: add root qdisc on %s: %v", ifaceID, err)
			return
		}
		// Default class — unlimited.
		if _, err := util.Exec(fmt.Sprintf(
			"tc class add dev %s parent 1: classid 1:%x htb rate %s",
			ifaceID, defaultClassID, defaultRate,
		), tcTimeout, false); err != nil {
			log.Printf("tc: add default class on %s: %v", ifaceID, err)
		}
	}

	// Ingress qdisc (upload limiting via police).
	if !strings.Contains(out, "ingress") {
		if _, err := util.Exec(fmt.Sprintf(
			"tc qdisc add dev %s ingress",
			ifaceID,
		), tcTimeout, false); err != nil && !strings.Contains(err.Error(), "File exists") {
			log.Printf("tc: add ingress qdisc on %s: %v", ifaceID, err)
		}
	}
}

// Apply sets (or updates) bandwidth limits for a peer on an interface.
// peerIP must be in CIDR notation (e.g. "10.8.0.5/32").
// rateDown and rateUp are in kbps; 0 means remove the limit for that direction.
// ifaceMTU is the WireGuard inner MTU of the interface (0 = use default 1420).
// Non-fatal: errors are logged but not returned.
//
// Update semantics: filters are always deleted and re-added; the HTB class is
// updated via "change" (atomically, without teardown) or created if absent.
// This avoids the "File exists" failure that would occur if the class delete
// had silently failed and we tried to re-add it.
func Apply(ifaceID, peerIP string, rateDown, rateUp, ifaceMTU int) {
	classID := ClassIDFromIP(peerIP)
	if classID == 0 {
		log.Printf("tc: cannot derive classid from IP %q — skipping", peerIP)
		return
	}
	host := strings.SplitN(peerIP, "/", 2)[0]

	// Step 1: remove existing filters (always — filters are on parent qdisc, not on class).
	// Egress filter: attached to root HTB qdisc (parent 1:), identified by priority.
	util.Exec(fmt.Sprintf( //nolint:errcheck
		"tc filter del dev %s parent 1: prio %d 2>/dev/null || true",
		ifaceID, classID,
	), tcTimeout, false)
	// Ingress filter: attached to ingress qdisc (parent ffff:), identified by priority.
	util.Exec(fmt.Sprintf( //nolint:errcheck
		"tc filter del dev %s parent ffff: prio %d 2>/dev/null || true",
		ifaceID, classID,
	), tcTimeout, false)

	if rateDown <= 0 && rateUp <= 0 {
		// No limits — also remove the HTB class if it exists.
		util.Exec(fmt.Sprintf( //nolint:errcheck
			"tc class del dev %s classid 1:%x 2>/dev/null || true",
			ifaceID, classID,
		), tcTimeout, false)
		return
	}

	// Step 2: egress / download limit.
	// NOTE: tc parses classid/flowid/handle as hexadecimal, prio/rate/burst as decimal.
	if rateDown > 0 {
		// Compensate for WireGuard overhead so the user-visible speed matches the target.
		tcRate := tcKbps(rateDown, ifaceMTU)
		burst := burstBytes(tcRate)
		// Use "change" to update an existing class, fall back to "add" for new peers.
		// This is atomic and avoids "File exists" when the previous class delete failed.
		classCmd := fmt.Sprintf(
			"tc class change dev %s parent 1: classid 1:%x htb rate %dkbit ceil %dkbit burst %db 2>/dev/null || "+
				"tc class add dev %s parent 1: classid 1:%x htb rate %dkbit ceil %dkbit burst %db",
			ifaceID, classID, tcRate, tcRate, burst,
			ifaceID, classID, tcRate, tcRate, burst,
		)
		if _, err := util.Exec(classCmd, tcTimeout, false); err != nil {
			log.Printf("tc: set class 1:%x on %s: %v", classID, ifaceID, err)
			return
		}
		if _, err := util.Exec(fmt.Sprintf(
			"tc filter add dev %s protocol ip parent 1: prio %d u32 match ip dst %s/32 flowid 1:%x",
			ifaceID, classID, host, classID,
		), tcTimeout, false); err != nil {
			log.Printf("tc: add egress filter for %s on %s: %v", host, ifaceID, err)
		}
	} else {
		// rateDown removed — delete class if it exists.
		util.Exec(fmt.Sprintf( //nolint:errcheck
			"tc class del dev %s classid 1:%x 2>/dev/null || true",
			ifaceID, classID,
		), tcTimeout, false)
	}

	// Step 3: ingress / upload limit (police filter).
	if rateUp > 0 {
		tcRate := tcKbps(rateUp, ifaceMTU)
		burst := burstBytes(tcRate)
		if _, err := util.Exec(fmt.Sprintf(
			"tc filter add dev %s parent ffff: protocol ip prio %d u32 match ip src %s/32 police rate %dkbit burst %db drop flowid :1",
			ifaceID, classID, host, tcRate, burst,
		), tcTimeout, false); err != nil {
			log.Printf("tc: add ingress filter for %s on %s: %v", host, ifaceID, err)
		}
	}
}

// Remove removes all tc rules for a peer identified by its VPN IP.
// Non-fatal.
func Remove(ifaceID, peerIP string) {
	// Apply with zero rates — cleans up filters and class.
	Apply(ifaceID, peerIP, 0, 0, 0)
}

// PeerLimit holds the rate limit info needed for tc restoration.
type PeerLimit struct {
	IP       string // VPN IP in CIDR notation, e.g. "10.8.0.5/32"
	RateDown int    // kbps, 0 = unlimited
	RateUp   int    // kbps, 0 = unlimited
}

// RestoreAll re-applies tc rules for a set of peers after an interface restart.
// ifaceMTU is the actual WireGuard inner MTU (0 = use default 1420).
// Call this after wg-quick up. Only peers with at least one non-zero rate are processed.
func RestoreAll(ifaceID string, limits []PeerLimit, ifaceMTU int) {
	if len(limits) == 0 {
		return
	}
	EnsureQdisc(ifaceID)
	for _, l := range limits {
		Apply(ifaceID, l.IP, l.RateDown, l.RateUp, ifaceMTU)
	}
}

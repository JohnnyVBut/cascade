package tunnel

import (
	"fmt"
	"net"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/awgparams"
	"github.com/JohnnyVBut/cascade/internal/settings"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// newTestManager builds a Manager with pre-populated interfaces for testing
// without touching the DB or the filesystem.
func newTestManager(addresses ...string) *Manager {
	m := &Manager{
		interfaces: make(map[string]*TunnelInterface),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
	for i, addr := range addresses {
		id := fmt.Sprintf("wg%d", 10+i)
		m.interfaces[id] = &TunnelInterface{
			ID:         id,
			Address:    addr,
			ListenPort: 51830 + i,
		}
	}
	return m
}

// ── nextSubnet ────────────────────────────────────────────────────────────────

func TestNextSubnet_EmptyPool(t *testing.T) {
	m := newTestManager()
	got, err := m.nextSubnet("192.168.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "192.168.0.1/24" {
		t.Errorf("got %q, want %q", got, "192.168.0.1/24")
	}
}

func TestNextSubnet_FirstAvailable(t *testing.T) {
	// Occupy the first /24 (192.168.0.x).
	m := newTestManager("192.168.0.1/24")
	got, err := m.nextSubnet("192.168.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "192.168.1.1/24" {
		t.Errorf("got %q, want %q", got, "192.168.1.1/24")
	}
}

func TestNextSubnet_SkipsUsed(t *testing.T) {
	// Occupy 192.168.0.x and 192.168.1.x; next should be 192.168.2.1/24.
	m := newTestManager("192.168.0.5/24", "192.168.1.1/24")
	got, err := m.nextSubnet("192.168.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "192.168.2.1/24" {
		t.Errorf("got %q, want %q", got, "192.168.2.1/24")
	}
}

func TestNextSubnet_PoolTooSmall(t *testing.T) {
	// A /25 pool is smaller than /24, so an error is expected.
	m := newTestManager()
	_, err := m.nextSubnet("192.168.0.0/25")
	if err == nil {
		t.Fatal("expected error for /25 pool (smaller than /24), got nil")
	}
}

func TestNextSubnet_Tiny24Pool(t *testing.T) {
	// A /24 pool is exactly one /24 block.
	m := newTestManager()
	got, err := m.nextSubnet("10.0.0.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "10.0.0.1/24" {
		t.Errorf("got %q, want %q", got, "10.0.0.1/24")
	}
}

func TestNextSubnet_PoolExhausted(t *testing.T) {
	// /24 pool with that single /24 already occupied.
	m := newTestManager("10.0.0.1/24")
	_, err := m.nextSubnet("10.0.0.0/24")
	if err == nil {
		t.Fatal("expected pool-exhausted error, got nil")
	}
}

func TestNextSubnet_NonHostBitsInAddress(t *testing.T) {
	// Interface address 10.0.1.5/24 → network is 10.0.1.0 → key {10,0,1,0}
	// Both 10.0.0.x and 10.0.1.x are occupied; expect 10.0.2.1/24.
	m := newTestManager("10.0.0.1/24", "10.0.1.5/24")
	got, err := m.nextSubnet("10.0.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "10.0.2.1/24" {
		t.Errorf("got %q, want %q", got, "10.0.2.1/24")
	}
}

func TestNextSubnet_InvalidCIDR(t *testing.T) {
	m := newTestManager()
	_, err := m.nextSubnet("not-a-cidr")
	if err == nil {
		t.Fatal("expected parse error for invalid CIDR, got nil")
	}
}

// ── ipToUint32 ────────────────────────────────────────────────────────────────

func TestIpToUint32(t *testing.T) {
	cases := []struct {
		ip   string
		want uint32
	}{
		{"0.0.0.0", 0},
		{"0.0.0.1", 1},
		{"1.0.0.0", 0x01000000},
		{"192.168.1.1", 0xC0A80101},
		{"255.255.255.255", 0xFFFFFFFF},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip).To4()
		got := ipToUint32(ip)
		if got != c.want {
			t.Errorf("ipToUint32(%q) = %08X, want %08X", c.ip, got, c.want)
		}
	}
}

// ── nextListenPortFromPool ────────────────────────────────────────────────────

func TestNextListenPortFromPool_ReturnsFirstFreeBindable(t *testing.T) {
	m := newTestManager()
	// Use a range of high ports unlikely to be in use.
	port, err := m.nextListenPortFromPool("59990-59995")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port < 59990 || port > 59995 {
		t.Errorf("port %d outside expected range 59990-59995", port)
	}
}

func TestNextListenPortFromPool_SkipsUsedByInterface(t *testing.T) {
	m := &Manager{
		interfaces: map[string]*TunnelInterface{
			"wg10": {ID: "wg10", Address: "10.0.0.1/24", ListenPort: 59980},
		},
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	// The first port (59980) is occupied by wg10; must return 59981 or 59982.
	port, err := m.nextListenPortFromPool("59980-59982")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port == 59980 {
		t.Errorf("returned port %d which is already used by wg10", port)
	}
	if port < 59981 || port > 59982 {
		t.Errorf("port %d outside expected fallback range 59981-59982", port)
	}
}

func TestNextListenPortFromPool_InvalidPool(t *testing.T) {
	m := newTestManager()
	_, err := m.nextListenPortFromPool("not-a-port")
	if err == nil {
		t.Fatal("expected parse error for invalid pool, got nil")
	}
}

// ── nextListenPort (legacy auto-assign) ──────────────────────────────────────

func TestNextListenPort_Empty(t *testing.T) {
	m := &Manager{
		interfaces: make(map[string]*TunnelInterface),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
	port := m.nextListenPort()
	if port != 51830 {
		t.Errorf("got %d, want 51830 (first port in empty map)", port)
	}
}

func TestNextListenPort_SkipsUsed(t *testing.T) {
	m := &Manager{
		interfaces: map[string]*TunnelInterface{
			"wg10": {ID: "wg10", ListenPort: 51830},
			"wg11": {ID: "wg11", ListenPort: 51831},
		},
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	port := m.nextListenPort()
	if port != 51832 {
		t.Errorf("got %d, want 51832", port)
	}
}

// ── nextInterfaceID ───────────────────────────────────────────────────────────

func TestNextInterfaceID_Empty(t *testing.T) {
	m := &Manager{
		interfaces: make(map[string]*TunnelInterface),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
	id := m.nextInterfaceID()
	if id != "wg10" {
		t.Errorf("got %q, want %q", id, "wg10")
	}
}

func TestNextInterfaceID_SkipsExisting(t *testing.T) {
	m := &Manager{
		interfaces: map[string]*TunnelInterface{
			"wg10": {},
			"wg11": {},
		},
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	id := m.nextInterfaceID()
	if id != "wg12" {
		t.Errorf("got %q, want %q", id, "wg12")
	}
}

// ── awg2ParamsFromTemplate ────────────────────────────────────────────────────

func TestAwg2ParamsFromTemplate_FieldMapping(t *testing.T) {
	tpl := &settings.AWG2Params{
		Jc: 5, Jmin: 10, Jmax: 100,
		S1: 20, S2: 25, S3: 30, S4: 35,
		H1: "h1val", H2: "h2val", H3: "h3val", H4: "h4val",
		I1: "i1val", I2: "i2val", I3: "i3val", I4: "i4val", I5: "i5val",
	}
	got := awg2ParamsFromTemplate(tpl)
	if got.Jc != 5 {
		t.Errorf("Jc: got %d, want 5", got.Jc)
	}
	if got.S1 != 20 {
		t.Errorf("S1: got %d, want 20", got.S1)
	}
	if got.H1 != "h1val" {
		t.Errorf("H1: got %q, want %q", got.H1, "h1val")
	}
	if got.I5 != "i5val" {
		t.Errorf("I5: got %q, want %q", got.I5, "i5val")
	}
}

// ── awg2ParamsFromGenerated ───────────────────────────────────────────────────

func TestAwg2ParamsFromGenerated_FieldMapping(t *testing.T) {
	from := &awgparams.Params{
		Jc: 7, Jmin: 50, Jmax: 1000,
		S1: 100, S2: 105, S3: 110, S4: 115,
		H1: "a", H2: "b", H3: "c", H4: "d",
		I1: "x", I2: "y", I3: "z", I4: "q", I5: "r",
	}
	got := awg2ParamsFromGenerated(from)
	if got.Jc != 7 {
		t.Errorf("Jc: got %d, want 7", got.Jc)
	}
	if got.S4 != 115 {
		t.Errorf("S4: got %d, want 115", got.S4)
	}
	if got.I1 != "x" {
		t.Errorf("I1: got %q, want %q", got.I1, "x")
	}
	if got.I5 != "r" {
		t.Errorf("I5: got %q, want %q", got.I5, "r")
	}
}

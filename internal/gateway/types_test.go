package gateway

import (
	"encoding/json"
	"testing"
)

// ── Gateway JSON marshaling ───────────────────────────────────────────────────

func TestGateway_JSONRoundTrip(t *testing.T) {
	gw := Gateway{
		ID:              "gw-1",
		Name:            "Moscow-01",
		Interface:       "eth0",
		GatewayIP:       "192.168.1.1",
		MonitorAddress:  "8.8.8.8",
		Enabled:         true,
		Monitor:         true,
		MonitorInterval: 10,
		WindowSeconds:   30,
		LatencyThreshold: 200,
		MonitorHttp: MonitorHttpConfig{
			Enabled:        true,
			URL:            "https://example.com/health",
			ExpectedStatus: 200,
			Interval:       10,
			Timeout:        5,
		},
		MonitorRule: "icmp_only",
		Description: "Primary gateway",
		CreatedAt:   "2026-01-01T00:00:00Z",
	}

	data, err := json.Marshal(gw)
	if err != nil {
		t.Fatalf("json.Marshal Gateway: %v", err)
	}

	var got Gateway
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal Gateway: %v", err)
	}

	if got.ID != gw.ID {
		t.Errorf("ID = %q, want %q", got.ID, gw.ID)
	}
	if got.Name != gw.Name {
		t.Errorf("Name = %q, want %q", got.Name, gw.Name)
	}
	if got.GatewayIP != gw.GatewayIP {
		t.Errorf("GatewayIP = %q, want %q", got.GatewayIP, gw.GatewayIP)
	}
	if !got.Enabled {
		t.Error("Enabled should be true")
	}
	if !got.MonitorHttp.Enabled {
		t.Error("MonitorHttp.Enabled should be true")
	}
	if got.MonitorHttp.URL != gw.MonitorHttp.URL {
		t.Errorf("MonitorHttp.URL = %q, want %q", got.MonitorHttp.URL, gw.MonitorHttp.URL)
	}
	if got.LatencyThreshold != 200 {
		t.Errorf("LatencyThreshold = %d, want 200", got.LatencyThreshold)
	}
}

// ── GatewayGroup JSON marshaling ─────────────────────────────────────────────

func TestGatewayGroup_JSONRoundTrip(t *testing.T) {
	grp := GatewayGroup{
		ID:          "grp-1",
		Name:        "KZ-Group",
		Trigger:     "packetloss",
		Description: "Kazakhstan gateways",
		Gateways: []GatewayGroupMember{
			{GatewayID: "gw-1", Tier: 1, Weight: 1},
			{GatewayID: "gw-2", Tier: 2, Weight: 1},
		},
		CreatedAt: "2026-01-01T00:00:00Z",
	}

	data, err := json.Marshal(grp)
	if err != nil {
		t.Fatalf("json.Marshal GatewayGroup: %v", err)
	}

	var got GatewayGroup
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal GatewayGroup: %v", err)
	}

	if got.ID != grp.ID {
		t.Errorf("ID = %q, want %q", got.ID, grp.ID)
	}
	if got.Trigger != "packetloss" {
		t.Errorf("Trigger = %q, want 'packetloss'", got.Trigger)
	}
	if len(got.Gateways) != 2 {
		t.Errorf("Gateways len = %d, want 2", len(got.Gateways))
	}
	if got.Gateways[0].GatewayID != "gw-1" {
		t.Errorf("first member GatewayID = %q, want 'gw-1'", got.Gateways[0].GatewayID)
	}
	if got.Gateways[1].Tier != 2 {
		t.Errorf("second member Tier = %d, want 2", got.Gateways[1].Tier)
	}
}

// ── MonitorStatus JSON marshaling ─────────────────────────────────────────────

func TestMonitorStatus_JSONNullPointers(t *testing.T) {
	// nil pointer fields should marshal as JSON null.
	status := MonitorStatus{
		Status:    "unknown",
		Latency:   nil,
		PacketLoss: nil,
		LastCheck: nil,
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal MonitorStatus: %v", err)
	}

	// Verify round-trip.
	var got MonitorStatus
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal MonitorStatus: %v", err)
	}
	if got.Status != "unknown" {
		t.Errorf("Status = %q, want 'unknown'", got.Status)
	}
	if got.Latency != nil {
		t.Errorf("Latency should be nil, got %v", got.Latency)
	}
}

func TestMonitorStatus_WithValues(t *testing.T) {
	latency := 42
	loss := 5
	check := "2026-01-01T00:00:00Z"
	httpStatus := "healthy"
	httpLatency := 100
	httpCheck := "2026-01-01T00:00:01Z"
	httpCode := 200

	status := MonitorStatus{
		Status:        "healthy",
		Latency:       &latency,
		PacketLoss:    &loss,
		LastCheck:     &check,
		HttpStatus:    &httpStatus,
		HttpLatency:   &httpLatency,
		HttpLastCheck: &httpCheck,
		HttpCode:      &httpCode,
	}

	data, _ := json.Marshal(status)
	var got MonitorStatus
	json.Unmarshal(data, &got)

	if got.Status != "healthy" {
		t.Errorf("Status = %q", got.Status)
	}
	if got.Latency == nil || *got.Latency != 42 {
		t.Errorf("Latency = %v, want 42", got.Latency)
	}
	if got.PacketLoss == nil || *got.PacketLoss != 5 {
		t.Errorf("PacketLoss = %v, want 5", got.PacketLoss)
	}
	if got.HttpCode == nil || *got.HttpCode != 200 {
		t.Errorf("HttpCode = %v, want 200", got.HttpCode)
	}
}

// ── GatewayWithStatus composition ────────────────────────────────────────────

func TestGatewayWithStatus_JSONComposition(t *testing.T) {
	latency := 15
	loss := 0
	status := MonitorStatus{
		Status:     "healthy",
		Latency:    &latency,
		PacketLoss: &loss,
	}
	gws := GatewayWithStatus{
		Gateway: Gateway{
			ID:   "gw-x",
			Name: "TestGW",
		},
		MonitorStatus: status,
	}

	data, err := json.Marshal(gws)
	if err != nil {
		t.Fatalf("json.Marshal GatewayWithStatus: %v", err)
	}

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	// Both embedded structs should have their fields at the top level.
	if m["id"] != "gw-x" {
		t.Errorf("id = %v, want 'gw-x'", m["id"])
	}
	if m["status"] != "healthy" {
		t.Errorf("status = %v, want 'healthy'", m["status"])
	}
}

package settings

import (
	"fmt"
	"os"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/db"
)

func initTestDB(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "cascade-settings-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	if err := db.Init(dir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})
}

// ── GetSettings defaults ──────────────────────────────────────────────────────

func TestGetSettings_Defaults(t *testing.T) {
	initTestDB(t)

	s, err := GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if s.DNS != "1.1.1.1, 8.8.8.8" {
		t.Errorf("DNS default = %q, want '1.1.1.1, 8.8.8.8'", s.DNS)
	}
	if s.DefaultPersistentKeepalive != 25 {
		t.Errorf("DefaultPersistentKeepalive = %d, want 25", s.DefaultPersistentKeepalive)
	}
	if s.DefaultClientAllowedIPs != "0.0.0.0/0, ::/0" {
		t.Errorf("DefaultClientAllowedIPs = %q, want '0.0.0.0/0, ::/0'", s.DefaultClientAllowedIPs)
	}
	if s.GatewayWindowSeconds != 30 {
		t.Errorf("GatewayWindowSeconds = %d, want 30", s.GatewayWindowSeconds)
	}
	if s.PublicIPMode != "auto" {
		t.Errorf("PublicIPMode = %q, want 'auto'", s.PublicIPMode)
	}
}

// ── UpdateSettings round-trip ─────────────────────────────────────────────────

func TestUpdateSettings_RoundTrip(t *testing.T) {
	initTestDB(t)

	updates := map[string]any{
		"dns":                     "8.8.8.8, 8.8.4.4",
		"defaultPersistentKeepalive": 30,
		"routerName":              "Moscow-01",
	}
	s, err := UpdateSettings(updates)
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if s.DNS != "8.8.8.8, 8.8.4.4" {
		t.Errorf("DNS = %q, want '8.8.8.8, 8.8.4.4'", s.DNS)
	}
	if s.DefaultPersistentKeepalive != 30 {
		t.Errorf("DefaultPersistentKeepalive = %d, want 30", s.DefaultPersistentKeepalive)
	}
	if s.RouterName != "Moscow-01" {
		t.Errorf("RouterName = %q, want 'Moscow-01'", s.RouterName)
	}
}

func TestUpdateSettings_PublicIPMode(t *testing.T) {
	initTestDB(t)

	// Invalid mode should not change the field.
	UpdateSettings(map[string]any{"publicIPMode": "invalid"})
	s, _ := GetSettings()
	if s.PublicIPMode != "auto" {
		t.Errorf("publicIPMode should stay 'auto' on invalid value, got %q", s.PublicIPMode)
	}

	// Valid manual mode.
	UpdateSettings(map[string]any{"publicIPMode": "manual", "publicIPManual": "1.2.3.4"})
	s, _ = GetSettings()
	if s.PublicIPMode != "manual" {
		t.Errorf("publicIPMode = %q, want 'manual'", s.PublicIPMode)
	}
	if s.PublicIPManual != "1.2.3.4" {
		t.Errorf("publicIPManual = %q, want '1.2.3.4'", s.PublicIPManual)
	}
}

// ── Template CRUD ─────────────────────────────────────────────────────────────

func TestCreateTemplate_Basic(t *testing.T) {
	initTestDB(t)

	tmpl, err := CreateTemplate(Template{
		Name: "MyTemplate",
		Jc:   7,
		I1:   "<r 100>",
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if tmpl.ID == "" {
		t.Error("Template.ID should not be empty")
	}
	if tmpl.Name != "MyTemplate" {
		t.Errorf("Template.Name = %q, want 'MyTemplate'", tmpl.Name)
	}
	if tmpl.Jc != 7 {
		t.Errorf("Template.Jc = %d, want 7", tmpl.Jc)
	}
	// H1-H4 should be auto-generated (non-empty).
	for _, h := range []string{tmpl.H1, tmpl.H2, tmpl.H3, tmpl.H4} {
		if h == "" {
			t.Errorf("expected auto-generated H range, got empty string")
		}
	}
}

func TestCreateTemplate_DuplicateNameError(t *testing.T) {
	initTestDB(t)

	_, err := CreateTemplate(Template{Name: "Duplicate"})
	if err != nil {
		t.Fatalf("first CreateTemplate: %v", err)
	}

	_, err = CreateTemplate(Template{Name: "Duplicate"})
	if err == nil {
		t.Error("expected error for duplicate template name, got nil")
	}
}

func TestCreateTemplate_RequiresName(t *testing.T) {
	initTestDB(t)

	_, err := CreateTemplate(Template{})
	if err == nil {
		t.Error("expected error for empty template name, got nil")
	}
}

func TestGetTemplate_Found(t *testing.T) {
	initTestDB(t)

	created, _ := CreateTemplate(Template{Name: "Find-Me"})

	got, err := GetTemplate(created.ID)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil template")
	}
	if got.Name != "Find-Me" {
		t.Errorf("Name = %q, want 'Find-Me'", got.Name)
	}
}

func TestGetTemplate_NotFound(t *testing.T) {
	initTestDB(t)

	got, err := GetTemplate("non-existent-id")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent ID, got %+v", got)
	}
}

func TestDeleteTemplate_RemovesIt(t *testing.T) {
	initTestDB(t)

	tmpl, _ := CreateTemplate(Template{Name: "ToDelete"})
	if err := DeleteTemplate(tmpl.ID); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}

	got, _ := GetTemplate(tmpl.ID)
	if got != nil {
		t.Errorf("expected nil after DeleteTemplate, got %+v", got)
	}
}

func TestDeleteTemplate_NotFound_Error(t *testing.T) {
	initTestDB(t)

	err := DeleteTemplate("does-not-exist")
	if err == nil {
		t.Error("expected error for deleting non-existent template, got nil")
	}
}

func TestGetTemplates_OrderedByCreatedAt(t *testing.T) {
	initTestDB(t)

	CreateTemplate(Template{Name: "Alpha"})
	CreateTemplate(Template{Name: "Beta"})
	CreateTemplate(Template{Name: "Gamma"})

	list, err := GetTemplates()
	if err != nil {
		t.Fatalf("GetTemplates: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 templates, got %d", len(list))
	}
}

func TestSetDefaultTemplate(t *testing.T) {
	initTestDB(t)

	t1, _ := CreateTemplate(Template{Name: "T1"})
	t2, _ := CreateTemplate(Template{Name: "T2"})

	// Set T1 as default.
	_, err := SetDefaultTemplate(t1.ID)
	if err != nil {
		t.Fatalf("SetDefaultTemplate T1: %v", err)
	}

	def, err := GetDefaultTemplate()
	if err != nil {
		t.Fatalf("GetDefaultTemplate: %v", err)
	}
	if def == nil || def.ID != t1.ID {
		t.Errorf("expected default to be T1 (%s), got %v", t1.ID, def)
	}

	// Switch default to T2.
	SetDefaultTemplate(t2.ID)
	def, _ = GetDefaultTemplate()
	if def == nil || def.ID != t2.ID {
		t.Errorf("expected default to be T2 (%s), got %v", t2.ID, def)
	}

	// T1 should no longer be default.
	got1, _ := GetTemplate(t1.ID)
	if got1.IsDefault {
		t.Error("T1 should not be default after switching to T2")
	}
}

func TestUpdateTemplate_ChangeName(t *testing.T) {
	initTestDB(t)

	tmpl, _ := CreateTemplate(Template{Name: "Original"})

	updated, err := UpdateTemplate(tmpl.ID, map[string]any{"name": "Renamed"})
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if updated.Name != "Renamed" {
		t.Errorf("Name = %q, want 'Renamed'", updated.Name)
	}
}

func TestApplyTemplate_ReturnsParams(t *testing.T) {
	initTestDB(t)

	tmpl, _ := CreateTemplate(Template{
		Name: "ApplyMe",
		Jc:   9, Jmin: 200, Jmax: 1000,
		S1: 32, S2: 33, S3: 20, S4: 8,
		I1: "<r 100>",
	})

	params, err := ApplyTemplate(tmpl.ID)
	if err != nil {
		t.Fatalf("ApplyTemplate: %v", err)
	}
	if params.Jc != 9 {
		t.Errorf("Jc = %d, want 9", params.Jc)
	}
	if params.I1 != "<r 100>" {
		t.Errorf("I1 = %q, want '<r 100>'", params.I1)
	}
}

func TestGetPeerDefaults(t *testing.T) {
	initTestDB(t)

	pd, err := GetPeerDefaults()
	if err != nil {
		t.Fatalf("GetPeerDefaults: %v", err)
	}
	if pd.DNS != "1.1.1.1, 8.8.8.8" {
		t.Errorf("DNS = %q", pd.DNS)
	}
	if pd.PersistentKeepalive != 25 {
		t.Errorf("PersistentKeepalive = %d, want 25", pd.PersistentKeepalive)
	}
}

// ── ChartType ─────────────────────────────────────────────────────────────────

func TestGetSettings_ChartTypeDefault(t *testing.T) {
	initTestDB(t)

	s, err := GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if s.ChartType != 2 {
		t.Errorf("ChartType default = %d, want 2 (area)", s.ChartType)
	}
}

func TestUpdateSettings_ChartType(t *testing.T) {
	initTestDB(t)

	for _, valid := range []int{0, 1, 2, 3} {
		s, err := UpdateSettings(map[string]any{"chartType": valid})
		if err != nil {
			t.Fatalf("UpdateSettings chartType=%d: %v", valid, err)
		}
		if s.ChartType != valid {
			t.Errorf("ChartType = %d, want %d", s.ChartType, valid)
		}
	}
}

func TestUpdateSettings_ChartType_InvalidIgnored(t *testing.T) {
	initTestDB(t)

	// Set to known good value first.
	UpdateSettings(map[string]any{"chartType": 1})

	// Invalid values (out of range) should be ignored — field stays at previous value.
	for _, invalid := range []int{-1, 4, 99} {
		s, err := UpdateSettings(map[string]any{"chartType": invalid})
		if err != nil {
			t.Fatalf("UpdateSettings chartType=%d: %v", invalid, err)
		}
		if s.ChartType != 1 {
			t.Errorf("ChartType should remain 1 after invalid value %d, got %d", invalid, s.ChartType)
		}
	}
}

// ── SubnetPool + PortPool ─────────────────────────────────────────────────────

func TestGetSettings_SubnetPoolDefault(t *testing.T) {
	initTestDB(t)
	s, err := GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if s.SubnetPool != "192.168.0.0/16" {
		t.Errorf("SubnetPool default = %q, want '192.168.0.0/16'", s.SubnetPool)
	}
}

func TestGetSettings_PortPoolDefault(t *testing.T) {
	initTestDB(t)
	s, err := GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if s.PortPool != "51831-65535" {
		t.Errorf("PortPool default = %q, want '51831-65535'", s.PortPool)
	}
}

func TestUpdateSettings_SubnetPool_Valid(t *testing.T) {
	initTestDB(t)
	s, err := UpdateSettings(map[string]any{"subnetPool": "10.0.0.0/8"})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if s.SubnetPool != "10.0.0.0/8" {
		t.Errorf("SubnetPool = %q, want '10.0.0.0/8'", s.SubnetPool)
	}
}

func TestUpdateSettings_SubnetPool_InvalidIgnored(t *testing.T) {
	initTestDB(t)
	// Invalid CIDR should be ignored — default stays.
	s, err := UpdateSettings(map[string]any{"subnetPool": "not-a-cidr"})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if s.SubnetPool != "192.168.0.0/16" {
		t.Errorf("SubnetPool should stay default on invalid value, got %q", s.SubnetPool)
	}
}

func TestUpdateSettings_PortPool_Valid(t *testing.T) {
	initTestDB(t)
	s, err := UpdateSettings(map[string]any{"portPool": "51831-51840, 52000"})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if s.PortPool != "51831-51840, 52000" {
		t.Errorf("PortPool = %q, want '51831-51840, 52000'", s.PortPool)
	}
}

func TestUpdateSettings_PortPool_InvalidIgnored(t *testing.T) {
	initTestDB(t)
	s, err := UpdateSettings(map[string]any{"portPool": "not-a-port"})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if s.PortPool != "51831-65535" {
		t.Errorf("PortPool should stay default on invalid value, got %q", s.PortPool)
	}
}

func TestParsePortPool_Range(t *testing.T) {
	ports, err := ParsePortPool("51831-51835")
	if err != nil {
		t.Fatalf("ParsePortPool: %v", err)
	}
	if len(ports) != 5 {
		t.Errorf("expected 5 ports, got %d", len(ports))
	}
	if ports[0] != 51831 || ports[4] != 51835 {
		t.Errorf("unexpected ports: %v", ports)
	}
}

func TestParsePortPool_Single(t *testing.T) {
	ports, err := ParsePortPool("52000")
	if err != nil {
		t.Fatalf("ParsePortPool: %v", err)
	}
	if len(ports) != 1 || ports[0] != 52000 {
		t.Errorf("expected [52000], got %v", ports)
	}
}

func TestParsePortPool_Mixed(t *testing.T) {
	ports, err := ParsePortPool("51831-51833, 52000, 54321-54322")
	if err != nil {
		t.Fatalf("ParsePortPool: %v", err)
	}
	// 3 + 1 + 2 = 6 ports
	if len(ports) != 6 {
		t.Errorf("expected 6 ports, got %d: %v", len(ports), ports)
	}
}

func TestParsePortPool_Sorted(t *testing.T) {
	ports, err := ParsePortPool("54321, 51831-51833, 52000")
	if err != nil {
		t.Fatalf("ParsePortPool: %v", err)
	}
	for i := 1; i < len(ports); i++ {
		if ports[i] <= ports[i-1] {
			t.Errorf("ports not sorted at index %d: %v", i, ports)
		}
	}
}

func TestParsePortPool_Deduplication(t *testing.T) {
	ports, err := ParsePortPool("51831-51833, 51832-51834")
	if err != nil {
		t.Fatalf("ParsePortPool: %v", err)
	}
	// 51831,51832,51833,51834 — 4 unique
	if len(ports) != 4 {
		t.Errorf("expected 4 unique ports, got %d: %v", len(ports), ports)
	}
}

func TestParsePortPool_InvalidRange(t *testing.T) {
	if _, err := ParsePortPool("abc-def"); err == nil {
		t.Error("expected error for invalid range 'abc-def'")
	}
}

func TestParsePortPool_OutOfRange(t *testing.T) {
	if _, err := ParsePortPool("100"); err == nil {
		t.Error("expected error for port 100 (below 1024)")
	}
	if _, err := ParsePortPool("70000"); err == nil {
		t.Error("expected error for port 70000 (above 65535)")
	}
}

func TestParsePortPool_Empty(t *testing.T) {
	if _, err := ParsePortPool(""); err == nil {
		t.Error("expected error for empty pool")
	}
}

func TestParsePortPool_StartGreaterThanEnd(t *testing.T) {
	if _, err := ParsePortPool("51840-51831"); err == nil {
		t.Error("expected error for range where start > end")
	}
}

func TestParsePortPool_NegativeNumber(t *testing.T) {
	// "-1" starts with '-' at index 0, so idx == 0 — treated as single port, not range.
	if _, err := ParsePortPool("-1"); err == nil {
		t.Error("expected error for negative port -1")
	}
}

func TestParsePortPool_WhitespaceOnlySegment(t *testing.T) {
	// "51831, , 52000" has an empty segment after trim — should be silently skipped.
	ports, err := ParsePortPool("51831, , 52000")
	if err != nil {
		t.Fatalf("ParsePortPool: %v", err)
	}
	if len(ports) != 2 {
		t.Errorf("expected 2 ports, got %d: %v", len(ports), ports)
	}
}

func TestUpdateSettings_SubnetPool_HostBitsSet(t *testing.T) {
	initTestDB(t)
	// "192.168.1.5/16" has host bits set — must be rejected (FINDING-3).
	s, err := UpdateSettings(map[string]any{"subnetPool": "192.168.1.5/16"})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if s.SubnetPool != "192.168.0.0/16" {
		t.Errorf("SubnetPool with host bits should be rejected, got %q", s.SubnetPool)
	}
}

// ── generateRandomHRanges ─────────────────────────────────────────────────────

func TestGenerateRandomHRanges_NonOverlapping(t *testing.T) {
	for i := 0; i < 20; i++ {
		h := generateRandomHRanges()
		ranges := []string{h.H1, h.H2, h.H3, h.H4}
		parsed := make([][2]int, 4)
		for j, r := range ranges {
			var start, end int
			if _, err := fmt.Sscanf(r, "%d-%d", &start, &end); err != nil {
				t.Fatalf("iteration %d: bad range %q: %v", i, r, err)
			}
			parsed[j] = [2]int{start, end}
		}
		for j := 0; j < 4; j++ {
			for k := j + 1; k < 4; k++ {
				if parsed[j][0] <= parsed[k][1] && parsed[k][0] <= parsed[j][1] {
					t.Errorf("iteration %d: H%d [%d-%d] overlaps H%d [%d-%d]",
						i, j+1, parsed[j][0], parsed[j][1], k+1, parsed[k][0], parsed[k][1])
				}
			}
		}
	}
}

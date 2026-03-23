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

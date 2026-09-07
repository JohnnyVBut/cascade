// Tests for Template.Version scoping — the AWG 3.0 protocol redo (item 3-6
// of the test plan): CreateTemplate/UpdateTemplate version validation,
// GetTemplates version filtering, and ApplyDefaultTemplate version scoping.
package settings

import "testing"

// ── CreateTemplate ────────────────────────────────────────────────────────────

func TestCreateTemplate_VersionDefaultsTo2_0(t *testing.T) {
	initTestDB(t)

	tmpl, err := CreateTemplate(Template{Name: "NoVersionGiven"})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if tmpl.Version != "2.0" {
		t.Errorf("Version = %q, want '2.0' (default)", tmpl.Version)
	}
}

func TestCreateTemplate_InvalidVersionRejected(t *testing.T) {
	initTestDB(t)

	_, err := CreateTemplate(Template{Name: "BadVersion", Version: "4.0"})
	if err == nil {
		t.Fatal("expected error for invalid version '4.0', got nil")
	}
}

func TestCreateTemplate_EmptyStringVersionIsNotAnInvalidVersionError(t *testing.T) {
	// Empty version is a distinct case from "invalid" — it must default to
	// "2.0" rather than being rejected by the version validity check.
	initTestDB(t)

	tmpl, err := CreateTemplate(Template{Name: "EmptyVersion", Version: ""})
	if err != nil {
		t.Fatalf("CreateTemplate with empty version: %v", err)
	}
	if tmpl.Version != "2.0" {
		t.Errorf("Version = %q, want '2.0'", tmpl.Version)
	}
}

// TestCreateTemplate_2_0RejectsAnyAWG3Field checks the "any of the 7 fields"
// rule against several distinct fields (not just headerProtectionKey) to make
// sure hasAWG3Fields() isn't only checking one of them.
func TestCreateTemplate_2_0RejectsAnyAWG3Field(t *testing.T) {
	initTestDB(t)

	cases := []struct {
		name    string
		mutate  func(*Template)
	}{
		{"contentPaddingAddition", func(tp *Template) { tp.ContentPaddingAddition = "0-1" }},
		{"rekeyAfterTime", func(tp *Template) { tp.RekeyAfterTime = "1h-2h" }},
		{"rejectAfterTime", func(tp *Template) { tp.RejectAfterTime = "3h" }},
		{"keepaliveTimeout", func(tp *Template) { tp.KeepaliveTimeout = "15-25" }},
		{"maxHandshakeAttempts", func(tp *Template) { tp.MaxHandshakeAttempts = "20" }},
		{"rekeyTimeout", func(tp *Template) { tp.RekeyTimeout = "5-10" }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tp := Template{Name: "Reject-" + c.name}
			c.mutate(&tp)
			// Version left empty → defaults to "2.0" → must be rejected.
			_, err := CreateTemplate(tp)
			if err == nil {
				t.Errorf("expected error creating a 2.0 (default) template with %s set, got nil", c.name)
			}
		})
	}
}

// TestCreateTemplate_2_0Explicit_RejectsAWG3Field is the same check but with
// version="2.0" explicitly set (rather than relying on the default), to make
// sure the check isn't accidentally scoped only to the "version omitted" path.
func TestCreateTemplate_2_0Explicit_RejectsAWG3Field(t *testing.T) {
	initTestDB(t)

	_, err := CreateTemplate(Template{
		Name:                "Explicit2_0",
		Version:             "2.0",
		HeaderProtectionKey: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		S3:                  12,
		S4:                  12,
	})
	if err == nil {
		t.Fatal("expected error for explicit version=2.0 with headerProtectionKey set, got nil")
	}
}

func TestCreateTemplate_3_0AcceptsAWG3Fields(t *testing.T) {
	initTestDB(t)

	tmpl, err := CreateTemplate(Template{
		Name:                   "V3-WithFields",
		Version:                "3.0",
		S3:                     12,
		S4:                     12,
		HeaderProtectionKey:    "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		ContentPaddingAddition: "0-1",
	})
	if err != nil {
		t.Fatalf("CreateTemplate version=3.0 with AWG3 fields: %v", err)
	}
	if tmpl.Version != "3.0" {
		t.Errorf("Version = %q, want '3.0'", tmpl.Version)
	}
	if tmpl.HeaderProtectionKey == "" {
		t.Error("HeaderProtectionKey should be preserved")
	}
}

// TestCreateTemplate_3_0WithNoAWG3Fields_Accepted confirms version="3.0"
// doesn't REQUIRE any AWG3 field to be set — it only ALLOWS them.
func TestCreateTemplate_3_0WithNoAWG3Fields_Accepted(t *testing.T) {
	initTestDB(t)

	tmpl, err := CreateTemplate(Template{
		Name:    "V3-NoFields",
		Version: "3.0",
	})
	if err != nil {
		t.Fatalf("CreateTemplate version=3.0 with no AWG3 fields: %v", err)
	}
	if tmpl.Version != "3.0" {
		t.Errorf("Version = %q, want '3.0'", tmpl.Version)
	}
	if tmpl.HeaderProtectionKey != "" {
		t.Errorf("HeaderProtectionKey = %q, want empty", tmpl.HeaderProtectionKey)
	}
}

// ── UpdateTemplate ────────────────────────────────────────────────────────────

func TestUpdateTemplate_InvalidVersionRejected(t *testing.T) {
	initTestDB(t)

	tmpl, err := CreateTemplate(Template{Name: "ToBreak"})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	_, err = UpdateTemplate(tmpl.ID, map[string]any{"version": "9.9"})
	if err == nil {
		t.Fatal("expected error updating to invalid version '9.9', got nil")
	}
}

// TestUpdateTemplate_2_0RejectsAnyAWG3Field mirrors the CreateTemplate check
// on the update path, across multiple distinct fields.
func TestUpdateTemplate_2_0RejectsAnyAWG3Field(t *testing.T) {
	initTestDB(t)

	cases := []struct {
		name string
		key  string
		val  any
	}{
		{"contentPaddingAddition", "contentPaddingAddition", "0-1"},
		{"rejectAfterTime", "rejectAfterTime", "3h"},
		{"maxHandshakeAttempts", "maxHandshakeAttempts", "20"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := CreateTemplate(Template{Name: "Upd2_0-" + c.name})
			if err != nil {
				t.Fatalf("CreateTemplate: %v", err)
			}
			// Template stays at default version "2.0"; setting an AWG3 field
			// via update must be rejected.
			_, err = UpdateTemplate(tmpl.ID, map[string]any{c.key: c.val})
			if err == nil {
				t.Errorf("expected error updating a 2.0 template with %s set, got nil", c.name)
			}
		})
	}
}

func TestUpdateTemplate_3_0AcceptsAWG3Fields(t *testing.T) {
	initTestDB(t)

	tmpl, err := CreateTemplate(Template{Name: "Upd3_0", S3: 12, S4: 12})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	updated, err := UpdateTemplate(tmpl.ID, map[string]any{
		"version":         "3.0",
		"rejectAfterTime": "3h",
	})
	if err != nil {
		t.Fatalf("UpdateTemplate version=3.0 + AWG3 field: %v", err)
	}
	if updated.Version != "3.0" {
		t.Errorf("Version = %q, want '3.0'", updated.Version)
	}
	if updated.RejectAfterTime != "3h" {
		t.Errorf("RejectAfterTime = %q, want '3h'", updated.RejectAfterTime)
	}
}

// TestUpdateTemplate_DowngradeVersionWithFieldStillPresent_Rejected is the
// order-of-operations case explicitly called out in the test plan: a 3.0
// template with an AWG3 field set, then an update that sets version back to
// "2.0" WITHOUT clearing the field in the same call, must be rejected (not
// silently accepted because the update map only touched "version").
func TestUpdateTemplate_DowngradeVersionWithFieldStillPresent_Rejected(t *testing.T) {
	initTestDB(t)

	tmpl, err := CreateTemplate(Template{
		Name:                "DowngradeMe",
		Version:             "3.0",
		S3:                  12,
		S4:                  12,
		HeaderProtectionKey: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	// Update ONLY the version field — headerProtectionKey remains set on t
	// from the struct loaded inside UpdateTemplate (GetTemplate(id)).
	_, err = UpdateTemplate(tmpl.ID, map[string]any{"version": "2.0"})
	if err == nil {
		t.Fatal("expected error downgrading to version=2.0 while headerProtectionKey is still set, got nil")
	}

	// Confirm nothing was persisted — template should be unchanged.
	got, getErr := GetTemplate(tmpl.ID)
	if getErr != nil {
		t.Fatalf("GetTemplate: %v", getErr)
	}
	if got.Version != "3.0" {
		t.Errorf("Version after rejected downgrade = %q, want unchanged '3.0'", got.Version)
	}
}

// TestUpdateTemplate_DowngradeVersionAfterClearingFields_Accepted is the
// counterpart: clearing the AWG3 field AND downgrading the version in the
// same update call must succeed.
func TestUpdateTemplate_DowngradeVersionAfterClearingFields_Accepted(t *testing.T) {
	initTestDB(t)

	tmpl, err := CreateTemplate(Template{
		Name:                "DowngradeCleanly",
		Version:             "3.0",
		S3:                  12,
		S4:                  12,
		HeaderProtectionKey: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	updated, err := UpdateTemplate(tmpl.ID, map[string]any{
		"version":             "2.0",
		"headerProtectionKey": "",
	})
	if err != nil {
		t.Fatalf("UpdateTemplate downgrade with cleared field: %v", err)
	}
	if updated.Version != "2.0" {
		t.Errorf("Version = %q, want '2.0'", updated.Version)
	}
	if updated.HeaderProtectionKey != "" {
		t.Errorf("HeaderProtectionKey = %q, want empty after clearing", updated.HeaderProtectionKey)
	}
}

// ── GetTemplates version filter ───────────────────────────────────────────────

func TestGetTemplates_EmptyVersionReturnsAll(t *testing.T) {
	initTestDB(t)

	CreateTemplate(Template{Name: "T2", Version: "2.0"})
	CreateTemplate(Template{Name: "T3", Version: "3.0"})

	list, err := GetTemplates("")
	if err != nil {
		t.Fatalf("GetTemplates(\"\"): %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 templates regardless of version, got %d", len(list))
	}
}

func TestGetTemplates_FiltersByVersion(t *testing.T) {
	initTestDB(t)

	CreateTemplate(Template{Name: "T2-A", Version: "2.0"})
	CreateTemplate(Template{Name: "T2-B", Version: "2.0"})
	CreateTemplate(Template{Name: "T3-A", Version: "3.0"})

	list2, err := GetTemplates("2.0")
	if err != nil {
		t.Fatalf("GetTemplates(2.0): %v", err)
	}
	if len(list2) != 2 {
		t.Errorf("GetTemplates(2.0): expected 2 templates, got %d", len(list2))
	}
	for _, tp := range list2 {
		if tp.Version != "2.0" {
			t.Errorf("GetTemplates(2.0) returned a %s template: %+v", tp.Version, tp)
		}
	}

	list3, err := GetTemplates("3.0")
	if err != nil {
		t.Fatalf("GetTemplates(3.0): %v", err)
	}
	if len(list3) != 1 {
		t.Errorf("GetTemplates(3.0): expected 1 template, got %d", len(list3))
	}
	if list3[0].Name != "T3-A" {
		t.Errorf("GetTemplates(3.0) returned %q, want 'T3-A'", list3[0].Name)
	}
}

// ── ApplyDefaultTemplate version scoping ──────────────────────────────────────

func TestApplyDefaultTemplate_NoDefaultAtAll_ReturnsNilNoError(t *testing.T) {
	initTestDB(t)

	// No templates created at all.
	params, err := ApplyDefaultTemplate("2.0")
	if err != nil {
		t.Fatalf("ApplyDefaultTemplate(2.0) with no templates: %v", err)
	}
	if params != nil {
		t.Errorf("expected nil params when no default template exists, got %+v", params)
	}

	params, err = ApplyDefaultTemplate("3.0")
	if err != nil {
		t.Fatalf("ApplyDefaultTemplate(3.0) with no templates: %v", err)
	}
	if params != nil {
		t.Errorf("expected nil params when no default template exists, got %+v", params)
	}
}

// TestApplyDefaultTemplate_DefaultExistsButVersionMismatch_ReturnsNilNoError
// is the critical case: a default template exists but is the wrong version —
// this must return (nil, nil), not an error and not the wrong-version params,
// since tunnel.Manager.buildAWG2Params relies on nil to trigger its
// random-fallback path.
func TestApplyDefaultTemplate_DefaultExistsButVersionMismatch_ReturnsNilNoError(t *testing.T) {
	initTestDB(t)

	tmpl, err := CreateTemplate(Template{Name: "DefaultV2", Version: "2.0", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	def, err := GetDefaultTemplate("2.0")
	if err != nil || def == nil || def.ID != tmpl.ID {
		t.Fatalf("precondition failed: default template not set correctly (def=%+v, err=%v)", def, err)
	}

	// Requesting version "3.0" while the default is "2.0" → nil, nil.
	params, err := ApplyDefaultTemplate("3.0")
	if err != nil {
		t.Fatalf("ApplyDefaultTemplate(3.0) with mismatched 2.0 default: %v", err)
	}
	if params != nil {
		t.Errorf("expected nil params for version-mismatched default template, got %+v", params)
	}

	// Sanity: requesting the matching version DOES return params.
	params, err = ApplyDefaultTemplate("2.0")
	if err != nil {
		t.Fatalf("ApplyDefaultTemplate(2.0) with matching default: %v", err)
	}
	if params == nil {
		t.Fatal("expected non-nil params for version-matched default template, got nil")
	}
}

func TestApplyDefaultTemplate_VersionMatchReturnsParams(t *testing.T) {
	initTestDB(t)

	_, err := CreateTemplate(Template{
		Name: "DefaultV3", Version: "3.0", IsDefault: true,
		S3: 12, S4: 12,
		HeaderProtectionKey: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	params, err := ApplyDefaultTemplate("3.0")
	if err != nil {
		t.Fatalf("ApplyDefaultTemplate(3.0): %v", err)
	}
	if params == nil {
		t.Fatal("expected non-nil params")
	}
	if params.HeaderProtectionKey == "" {
		t.Error("expected HeaderProtectionKey to be carried through from the default template")
	}
}

package aliases

import (
	"os"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/ipset"
)

func initTestDB(t *testing.T) *Manager {
	t.Helper()
	dir, err := os.MkdirTemp("", "cascade-aliases-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	if err := db.Init(dir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	im, err := ipset.New(dir)
	if err != nil {
		t.Fatalf("ipset.New: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})
	return New(im)
}

// ── validateName ──────────────────────────────────────────────────────────────

func TestValidateName_Valid(t *testing.T) {
	cases := []string{
		"MyAlias",
		"alias1",
		"vpn_ru",
		"my-net",
		"a",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateName(name); err != nil {
				t.Errorf("validateName(%q) returned unexpected error: %v", name, err)
			}
		})
	}
}

func TestValidateName_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"starts with digit", "1alias"},
		{"starts with dash", "-alias"},
		{"too long (64 chars)", "a123456789012345678901234567890123456789012345678901234567890123"},
		{"shell injection", "alias; id"},
		{"space", "my alias"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateName(tc.input); err == nil {
				t.Errorf("validateName(%q) expected error, got nil", tc.input)
			}
		})
	}
}

// ── validateType ──────────────────────────────────────────────────────────────

func TestValidateType_Valid(t *testing.T) {
	for _, tp := range []string{"host", "network", "ipset", "group", "port", "port-group"} {
		t.Run(tp, func(t *testing.T) {
			if err := validateType(tp); err != nil {
				t.Errorf("validateType(%q) returned unexpected error: %v", tp, err)
			}
		})
	}
}

func TestValidateType_Invalid(t *testing.T) {
	for _, tp := range []string{"", "unknown", "Host", "CIDR"} {
		t.Run(tp, func(t *testing.T) {
			if err := validateType(tp); err == nil {
				t.Errorf("validateType(%q) expected error, got nil", tp)
			}
		})
	}
}

// ── ipsetNameFromAlias ────────────────────────────────────────────────────────

func TestIpsetNameFromAlias(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"MySet", "myset"},
		{"My-Set", "my_set"},
		{"long_name_that_exceeds_the_limit_of_31_chars", "long_name_that_exceeds_the_limi"},
		{"abc", "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := ipsetNameFromAlias(tc.input)
			if got != tc.want {
				t.Errorf("ipsetNameFromAlias(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ── dedupeStrings ─────────────────────────────────────────────────────────────

func TestDedupeStrings(t *testing.T) {
	input := []string{"a", "b", "a", "c", "b"}
	got := dedupeStrings(input)
	if len(got) != 3 {
		t.Errorf("dedupeStrings: expected 3, got %d: %v", len(got), got)
	}
	seen := map[string]int{}
	for _, v := range got {
		seen[v]++
	}
	for _, v := range []string{"a", "b", "c"} {
		if seen[v] != 1 {
			t.Errorf("dedupeStrings: %q appears %d times, want 1", v, seen[v])
		}
	}
}

// ── normalizePortEntries ──────────────────────────────────────────────────────

func TestNormalizePortEntries_Valid(t *testing.T) {
	cases := []struct {
		input []string
		want  []string
	}{
		{[]string{"tcp:443"}, []string{"tcp:443"}},
		{[]string{"UDP:53"}, []string{"udp:53"}},
		{[]string{"any:80"}, []string{"any:80"}},
		{[]string{"tcp:8080-8090"}, []string{"tcp:8080-8090"}},
		{[]string{"  tcp:443  "}, []string{"tcp:443"}}, // trim whitespace
	}
	for _, tc := range cases {
		out, err := normalizePortEntries(tc.input)
		if err != nil {
			t.Errorf("normalizePortEntries(%v) error: %v", tc.input, err)
			continue
		}
		if len(out) != len(tc.want) {
			t.Errorf("normalizePortEntries(%v) len=%d, want %d", tc.input, len(out), len(tc.want))
			continue
		}
		for i := range out {
			if out[i] != tc.want[i] {
				t.Errorf("normalizePortEntries(%v)[%d] = %q, want %q", tc.input, i, out[i], tc.want[i])
			}
		}
	}
}

func TestNormalizePortEntries_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input []string
	}{
		{"bad format", []string{"443"}},
		{"invalid proto", []string{"icmp:8"}},
		{"port zero", []string{"tcp:0"}},
		{"port too large", []string{"tcp:65536"}},
		{"range reversed", []string{"tcp:9000-8000"}},
		{"range end out of range", []string{"tcp:100-99999"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizePortEntries(tc.input); err == nil {
				t.Errorf("normalizePortEntries(%v) expected error, got nil", tc.input)
			}
		})
	}
}

// ── CRUD ─────────────────────────────────────────────────────────────────────

func TestCreate_HostAlias(t *testing.T) {
	m := initTestDB(t)

	a, err := m.Create(Alias{
		Name:    "MyHosts",
		Type:    "host",
		Entries: []string{"1.2.3.4", "5.6.7.8"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.ID == "" {
		t.Error("expected non-empty ID")
	}
	if len(a.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(a.Entries))
	}
	if a.EntryCount != 2 {
		t.Errorf("expected EntryCount=2, got %d", a.EntryCount)
	}
}

func TestCreate_DuplicateNameError(t *testing.T) {
	m := initTestDB(t)

	m.Create(Alias{Name: "Dup", Type: "host"})
	_, err := m.Create(Alias{Name: "Dup", Type: "network"})
	if err == nil {
		t.Error("expected error for duplicate alias name, got nil")
	}
}

func TestCreate_PortAlias(t *testing.T) {
	m := initTestDB(t)

	a, err := m.Create(Alias{
		Name:    "WebPorts",
		Type:    "port",
		Entries: []string{"tcp:80", "tcp:443"},
	})
	if err != nil {
		t.Fatalf("Create port alias: %v", err)
	}
	if len(a.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(a.Entries))
	}
}

func TestGetByID_Found(t *testing.T) {
	m := initTestDB(t)

	created, _ := m.Create(Alias{Name: "FindMe", Type: "network", Entries: []string{"10.0.0.0/8"}})

	got, err := m.GetByID(created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil alias")
	}
	if got.Name != "FindMe" {
		t.Errorf("Name = %q, want 'FindMe'", got.Name)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	m := initTestDB(t)

	got, err := m.GetByID("non-existent-id")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for unknown ID, got %+v", got)
	}
}

func TestGetByName_CaseInsensitive(t *testing.T) {
	m := initTestDB(t)

	m.Create(Alias{Name: "CaseSensitive", Type: "host"})

	got, err := m.GetByName("casesensitive")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got == nil {
		t.Fatal("expected alias via case-insensitive lookup")
	}
}

func TestGetAll_ReturnsCreated(t *testing.T) {
	m := initTestDB(t)

	m.Create(Alias{Name: "One", Type: "host"})
	m.Create(Alias{Name: "Two", Type: "network"})

	all, err := m.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 aliases, got %d", len(all))
	}
}

func TestUpdate_ChangeName(t *testing.T) {
	m := initTestDB(t)

	a, _ := m.Create(Alias{Name: "OldName", Type: "host"})
	updated, err := m.Update(a.ID, Alias{Name: "NewName"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "NewName" {
		t.Errorf("Name = %q, want 'NewName'", updated.Name)
	}
}

func TestDelete_Removes(t *testing.T) {
	m := initTestDB(t)

	a, _ := m.Create(Alias{Name: "ToDelete", Type: "host"})
	if err := m.Delete(a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, _ := m.GetByID(a.ID)
	if got != nil {
		t.Errorf("expected nil after Delete, got %+v", got)
	}
}

func TestDelete_RejectedIfUsedInGroup(t *testing.T) {
	m := initTestDB(t)

	member, _ := m.Create(Alias{Name: "MemberHost", Type: "host"})
	grp, _ := m.Create(Alias{
		Name:      "MyGroup",
		Type:      "group",
		MemberIDs: []string{member.ID},
	})
	_ = grp

	err := m.Delete(member.ID)
	if err == nil {
		t.Error("expected error when deleting alias used in a group, got nil")
	}
}

// ── GetMatchSpec ──────────────────────────────────────────────────────────────

func TestGetMatchSpec_HostAlias(t *testing.T) {
	m := initTestDB(t)

	a, _ := m.Create(Alias{
		Name:    "Hosts",
		Type:    "host",
		Entries: []string{"1.2.3.4", "5.6.7.8"},
	})

	spec, err := m.GetMatchSpec(a.ID)
	if err != nil {
		t.Fatalf("GetMatchSpec: %v", err)
	}
	if spec.Type != "cidr" {
		t.Errorf("Type = %q, want 'cidr'", spec.Type)
	}
	if len(spec.Entries) != 2 {
		t.Errorf("Entries len = %d, want 2", len(spec.Entries))
	}
}

func TestGetMatchSpec_GroupMergesMembers(t *testing.T) {
	m := initTestDB(t)

	h1, _ := m.Create(Alias{Name: "H1", Type: "host", Entries: []string{"1.1.1.1", "2.2.2.2"}})
	h2, _ := m.Create(Alias{Name: "H2", Type: "host", Entries: []string{"3.3.3.3"}})
	grp, _ := m.Create(Alias{Name: "Grp", Type: "group", MemberIDs: []string{h1.ID, h2.ID}})

	spec, err := m.GetMatchSpec(grp.ID)
	if err != nil {
		t.Fatalf("GetMatchSpec group: %v", err)
	}
	if spec.Type != "cidr" {
		t.Errorf("Type = %q, want 'cidr'", spec.Type)
	}
	if len(spec.Entries) != 3 {
		t.Errorf("merged entries len = %d, want 3", len(spec.Entries))
	}
}

// ── GetPortMatchSpec ──────────────────────────────────────────────────────────

func TestGetPortMatchSpec_PortAlias(t *testing.T) {
	m := initTestDB(t)

	a, _ := m.Create(Alias{
		Name:    "HttpPorts",
		Type:    "port",
		Entries: []string{"tcp:80", "tcp:443"},
	})

	specs, err := m.GetPortMatchSpec(a.ID)
	if err != nil {
		t.Fatalf("GetPortMatchSpec: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 protocol group, got %d", len(specs))
	}
	if specs[0].Proto != "tcp" {
		t.Errorf("Proto = %q, want 'tcp'", specs[0].Proto)
	}
	if !specs[0].Multiport {
		t.Error("expected Multiport=true for 2 ports")
	}
}

func TestGetPortMatchSpec_AnyProtoExpandsToBoth(t *testing.T) {
	m := initTestDB(t)

	a, _ := m.Create(Alias{
		Name:    "DnsPort",
		Type:    "port",
		Entries: []string{"any:53"},
	})

	specs, err := m.GetPortMatchSpec(a.ID)
	if err != nil {
		t.Fatalf("GetPortMatchSpec: %v", err)
	}
	if len(specs) != 2 {
		t.Errorf("expected 2 specs (tcp+udp), got %d", len(specs))
	}
}

func TestGetPortMatchSpec_NonPortAliasError(t *testing.T) {
	m := initTestDB(t)

	a, _ := m.Create(Alias{Name: "Hosts", Type: "host"})
	_, err := m.GetPortMatchSpec(a.ID)
	if err == nil {
		t.Error("expected error for non-port alias, got nil")
	}
}

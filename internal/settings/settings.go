// Package settings manages global application settings and AWG2 templates.
// Mirrors Settings.js from the Node.js version.
// Storage: SQLite tables `settings` (key/value) and `templates`.
package settings

import (
	"database/sql"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/google/uuid"

	"github.com/JohnnyVBut/awg-easy/internal/db"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// GlobalSettings holds application-wide defaults.
// Mirrors the DEFAULTS object in Settings.js.
type GlobalSettings struct {
	DNS                      string  `json:"dns"`
	DefaultPersistentKeepalive int   `json:"defaultPersistentKeepalive"`
	DefaultClientAllowedIPs  string  `json:"defaultClientAllowedIPs"`
	GatewayWindowSeconds     int     `json:"gatewayWindowSeconds"`
	GatewayHealthyThreshold  float64 `json:"gatewayHealthyThreshold"`
	GatewayDegradedThreshold float64 `json:"gatewayDegradedThreshold"`
}

// Template is an AWG2 obfuscation parameter set.
type Template struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
	Jc        int    `json:"jc"`
	Jmin      int    `json:"jmin"`
	Jmax      int    `json:"jmax"`
	S1        int    `json:"s1"`
	S2        int    `json:"s2"`
	S3        int    `json:"s3"`
	S4        int    `json:"s4"`
	H1        string `json:"h1"`  // "start-end" range string (FIX-4)
	H2        string `json:"h2"`
	H3        string `json:"h3"`
	H4        string `json:"h4"`
	I1        string `json:"i1"`
	I2        string `json:"i2"`
	I3        string `json:"i3"`
	I4        string `json:"i4"`
	I5        string `json:"i5"`
	CreatedAt string `json:"createdAt"`
}

// AWG2Params is a flat set of AWG2 params returned by ApplyTemplate.
type AWG2Params struct {
	Jc   int    `json:"jc"`
	Jmin int    `json:"jmin"`
	Jmax int    `json:"jmax"`
	S1   int    `json:"s1"`
	S2   int    `json:"s2"`
	S3   int    `json:"s3"`
	S4   int    `json:"s4"`
	H1   string `json:"h1"`
	H2   string `json:"h2"`
	H3   string `json:"h3"`
	H4   string `json:"h4"`
	I1   string `json:"i1"`
	I2   string `json:"i2"`
	I3   string `json:"i3"`
	I4   string `json:"i4"`
	I5   string `json:"i5"`
}

// PeerDefaults are passed to InterfaceManager when creating a new peer.
type PeerDefaults struct {
	DNS                string `json:"dns"`
	PersistentKeepalive int   `json:"persistentKeepalive"`
	ClientAllowedIPs   string `json:"clientAllowedIPs"`
}

// defaults mirrors DEFAULTS in Settings.js.
var defaults = GlobalSettings{
	DNS:                      "1.1.1.1, 8.8.8.8",
	DefaultPersistentKeepalive: 25,
	DefaultClientAllowedIPs:  "0.0.0.0/0, ::/0",
	GatewayWindowSeconds:     30,
	GatewayHealthyThreshold:  95,
	GatewayDegradedThreshold: 90,
}

// ── Public API ────────────────────────────────────────────────────────────────

// GetSettings returns current global settings, falling back to defaults.
func GetSettings() (*GlobalSettings, error) {
	d := db.DB()
	s := defaults // copy defaults

	rows, err := d.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("settings query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		applySettingKey(&s, k, v)
	}

	return &s, nil
}

// UpdateSettings persists only the provided fields (partial update).
// Returns the updated settings.
func UpdateSettings(updates map[string]any) (*GlobalSettings, error) {
	d := db.DB()

	for k, raw := range updates {
		v := fmt.Sprintf("%v", raw)
		_, err := d.Exec(
			`INSERT INTO settings(key, value) VALUES(?,?)
			 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
			k, v,
		)
		if err != nil {
			return nil, fmt.Errorf("update setting %q: %w", k, err)
		}
	}

	return GetSettings()
}

// GetPeerDefaults returns dns/keepalive/allowedIPs for new peer creation.
func GetPeerDefaults() (*PeerDefaults, error) {
	s, err := GetSettings()
	if err != nil {
		return nil, err
	}
	return &PeerDefaults{
		DNS:                 s.DNS,
		PersistentKeepalive: s.DefaultPersistentKeepalive,
		ClientAllowedIPs:    s.DefaultClientAllowedIPs,
	}, nil
}

// ── Templates ─────────────────────────────────────────────────────────────────

// GetTemplates returns all templates ordered by created_at.
func GetTemplates() ([]Template, error) {
	rows, err := db.DB().Query(`
		SELECT id, name, is_default,
		       jc, jmin, jmax, s1, s2, s3, s4,
		       h1, h2, h3, h4, i1, i2, i3, i4, i5, created_at
		FROM templates ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("templates query: %w", err)
	}
	defer rows.Close()

	var out []Template
	for rows.Next() {
		var t Template
		var isDefault int
		if err := rows.Scan(
			&t.ID, &t.Name, &isDefault,
			&t.Jc, &t.Jmin, &t.Jmax,
			&t.S1, &t.S2, &t.S3, &t.S4,
			&t.H1, &t.H2, &t.H3, &t.H4,
			&t.I1, &t.I2, &t.I3, &t.I4, &t.I5,
			&t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("template scan: %w", err)
		}
		t.IsDefault = isDefault == 1
		out = append(out, t)
	}

	return out, nil
}

// GetTemplate returns a single template by id, or nil if not found.
func GetTemplate(id string) (*Template, error) {
	return queryTemplate(`WHERE id = ?`, id)
}

// GetDefaultTemplate returns the template marked as default, or nil.
func GetDefaultTemplate() (*Template, error) {
	return queryTemplate(`WHERE is_default = 1`)
}

// CreateTemplate creates a new template with random H1-H4 ranges if not provided.
// Mirrors Settings.createTemplate() from Node.js.
func CreateTemplate(data Template) (*Template, error) {
	if data.Name == "" {
		return nil, fmt.Errorf("template name is required")
	}

	// Unique name check (case-insensitive, mirrors Node.js behaviour).
	var count int
	if err := db.DB().QueryRow(
		`SELECT COUNT(*) FROM templates WHERE name = ? COLLATE NOCASE`, data.Name,
	).Scan(&count); err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, fmt.Errorf("template with name %q already exists", data.Name)
	}

	// Generate H1-H4 if not provided (FIX-4: non-overlapping zones).
	hr := generateRandomHRanges()
	if data.H1 == "" { data.H1 = hr.H1 }
	if data.H2 == "" { data.H2 = hr.H2 }
	if data.H3 == "" { data.H3 = hr.H3 }
	if data.H4 == "" { data.H4 = hr.H4 }

	// Apply defaults for numeric fields.
	if data.Jc == 0   { data.Jc = 6 }
	if data.Jmin == 0 { data.Jmin = 10 }
	if data.Jmax == 0 { data.Jmax = 50 }
	if data.S1 == 0   { data.S1 = 64 }
	if data.S2 == 0   { data.S2 = 67 }
	if data.S3 == 0   { data.S3 = 64 }
	if data.S4 == 0   { data.S4 = 4 }

	data.ID = uuid.NewString()
	data.CreatedAt = time.Now().UTC().Format(time.RFC3339)

	tx, err := db.DB().Begin()
	if err != nil {
		return nil, err
	}

	// If this is default — unset all others first.
	if data.IsDefault {
		if _, err := tx.Exec(`UPDATE templates SET is_default = 0`); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	_, err = tx.Exec(`
		INSERT INTO templates
		    (id, name, is_default, jc, jmin, jmax, s1, s2, s3, s4,
		     h1, h2, h3, h4, i1, i2, i3, i4, i5, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		data.ID, data.Name, boolInt(data.IsDefault),
		data.Jc, data.Jmin, data.Jmax,
		data.S1, data.S2, data.S3, data.S4,
		data.H1, data.H2, data.H3, data.H4,
		data.I1, data.I2, data.I3, data.I4, data.I5,
		data.CreatedAt,
	)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("insert template: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &data, nil
}

// UpdateTemplate applies partial updates to an existing template.
func UpdateTemplate(id string, updates map[string]any) (*Template, error) {
	t, err := GetTemplate(id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("template not found")
	}

	// Apply updates to the struct.
	if v, ok := updates["name"].(string); ok         { t.Name = v }
	if v, ok := updates["isDefault"].(bool); ok      { t.IsDefault = v }
	if v, ok := updates["jc"].(float64); ok          { t.Jc = int(v) }
	if v, ok := updates["jmin"].(float64); ok        { t.Jmin = int(v) }
	if v, ok := updates["jmax"].(float64); ok        { t.Jmax = int(v) }
	if v, ok := updates["s1"].(float64); ok          { t.S1 = int(v) }
	if v, ok := updates["s2"].(float64); ok          { t.S2 = int(v) }
	if v, ok := updates["s3"].(float64); ok          { t.S3 = int(v) }
	if v, ok := updates["s4"].(float64); ok          { t.S4 = int(v) }
	if v, ok := updates["h1"].(string); ok           { t.H1 = v }
	if v, ok := updates["h2"].(string); ok           { t.H2 = v }
	if v, ok := updates["h3"].(string); ok           { t.H3 = v }
	if v, ok := updates["h4"].(string); ok           { t.H4 = v }
	if v, ok := updates["i1"].(string); ok           { t.I1 = v }
	if v, ok := updates["i2"].(string); ok           { t.I2 = v }
	if v, ok := updates["i3"].(string); ok           { t.I3 = v }
	if v, ok := updates["i4"].(string); ok           { t.I4 = v }
	if v, ok := updates["i5"].(string); ok           { t.I5 = v }

	tx, err := db.DB().Begin()
	if err != nil {
		return nil, err
	}

	if t.IsDefault {
		if _, err := tx.Exec(`UPDATE templates SET is_default = 0`); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	_, err = tx.Exec(`
		UPDATE templates SET
		    name=?, is_default=?, jc=?, jmin=?, jmax=?,
		    s1=?, s2=?, s3=?, s4=?,
		    h1=?, h2=?, h3=?, h4=?,
		    i1=?, i2=?, i3=?, i4=?, i5=?
		WHERE id=?`,
		t.Name, boolInt(t.IsDefault),
		t.Jc, t.Jmin, t.Jmax,
		t.S1, t.S2, t.S3, t.S4,
		t.H1, t.H2, t.H3, t.H4,
		t.I1, t.I2, t.I3, t.I4, t.I5,
		id,
	)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("update template: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return t, nil
}

// DeleteTemplate removes a template by id.
func DeleteTemplate(id string) error {
	res, err := db.DB().Exec(`DELETE FROM templates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("template not found")
	}
	return nil
}

// SetDefaultTemplate marks one template as default, unsets all others.
func SetDefaultTemplate(id string) (*Template, error) {
	tx, err := db.DB().Begin()
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`UPDATE templates SET is_default = 0`); err != nil {
		tx.Rollback()
		return nil, err
	}

	res, err := tx.Exec(`UPDATE templates SET is_default = 1 WHERE id = ?`, id)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		tx.Rollback()
		return nil, fmt.Errorf("template not found")
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return GetTemplate(id)
}

// ApplyTemplate returns a copy of the template's AWG2 params.
// H1-H4 are copied as-is — both tunnel sides MUST use identical ranges (FIX-4).
func ApplyTemplate(id string) (*AWG2Params, error) {
	t, err := GetTemplate(id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("template not found")
	}
	return templateToParams(t), nil
}

// ApplyDefaultTemplate returns params from the default template, or nil if none set.
func ApplyDefaultTemplate() (*AWG2Params, error) {
	t, err := GetDefaultTemplate()
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, nil
	}
	return templateToParams(t), nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func queryTemplate(where string, args ...any) (*Template, error) {
	row := db.DB().QueryRow(`
		SELECT id, name, is_default,
		       jc, jmin, jmax, s1, s2, s3, s4,
		       h1, h2, h3, h4, i1, i2, i3, i4, i5, created_at
		FROM templates `+where, args...)

	var t Template
	var isDefault int
	err := row.Scan(
		&t.ID, &t.Name, &isDefault,
		&t.Jc, &t.Jmin, &t.Jmax,
		&t.S1, &t.S2, &t.S3, &t.S4,
		&t.H1, &t.H2, &t.H3, &t.H4,
		&t.I1, &t.I2, &t.I3, &t.I4, &t.I5,
		&t.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("template scan: %w", err)
	}
	t.IsDefault = isDefault == 1
	return &t, nil
}

func templateToParams(t *Template) *AWG2Params {
	return &AWG2Params{
		Jc: t.Jc, Jmin: t.Jmin, Jmax: t.Jmax,
		S1: t.S1, S2: t.S2, S3: t.S3, S4: t.S4,
		H1: t.H1, H2: t.H2, H3: t.H3, H4: t.H4,
		I1: t.I1, I2: t.I2, I3: t.I3, I4: t.I4, I5: t.I5,
	}
}

// hRanges holds the result of generateRandomHRanges.
type hRanges struct{ H1, H2, H3, H4 string }

// generateRandomHRanges generates 4 non-overlapping H1-H4 ranges.
// Mirrors FIX-4 exactly: uint32 space divided into 4 equal zones,
// each range spans ~50M values within its zone.
func generateRandomHRanges() hRanges {
	const rangeSize = 50_000_000
	zoneSize := int(math.Floor(float64(0xFFFFFFFF-5) / 4))
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	randRange := func(zone int) string {
		zoneStart := 5 + zone*zoneSize
		zoneEnd := zoneStart + zoneSize - 1
		start := zoneStart + r.Intn(zoneEnd-zoneStart-rangeSize)
		return fmt.Sprintf("%d-%d", start, start+rangeSize)
	}

	return hRanges{
		H1: randRange(0),
		H2: randRange(1),
		H3: randRange(2),
		H4: randRange(3),
	}
}

// applySettingKey applies a single k/v row from the settings table to a GlobalSettings struct.
func applySettingKey(s *GlobalSettings, k, v string) {
	switch k {
	case "dns":
		s.DNS = v
	case "defaultPersistentKeepalive":
		var n int
		fmt.Sscanf(v, "%d", &n)
		if n > 0 { s.DefaultPersistentKeepalive = n }
	case "defaultClientAllowedIPs":
		s.DefaultClientAllowedIPs = v
	case "gatewayWindowSeconds":
		var n int
		fmt.Sscanf(v, "%d", &n)
		if n > 0 { s.GatewayWindowSeconds = n }
	case "gatewayHealthyThreshold":
		var f float64
		fmt.Sscanf(v, "%f", &f)
		if f > 0 { s.GatewayHealthyThreshold = f }
	case "gatewayDegradedThreshold":
		var f float64
		fmt.Sscanf(v, "%f", &f)
		if f > 0 { s.GatewayDegradedThreshold = f }
	}
}

func boolInt(b bool) int {
	if b { return 1 }
	return 0
}

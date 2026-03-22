package gateway

// Manager handles CRUD for Gateway and GatewayGroup in SQLite,
// and controls the Monitor lifecycle for each gateway.
//
// Singleton pattern: call NewManager() once at startup, then Init()
// to load existing gateways from DB and start their monitors.
//
// JSON columns:
//   - gateways.monitor_http   → MonitorHttpConfig (JSON)
//   - gateway_groups.members  → []GatewayGroupMember (JSON)

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/JohnnyVBut/awg-easy/internal/db"
)

// Manager manages gateways and gateway groups.
type Manager struct {
	monitor *Monitor
}

// NewManager creates a Manager with a freshly created Monitor.
// Call Init() to restore previously saved gateways.
func NewManager() *Manager {
	return &Manager{monitor: NewMonitor()}
}

// Monitor exposes the embedded Monitor so callers (e.g. FirewallManager)
// can register StatusChangeFunc callbacks.
func (m *Manager) Monitor() *Monitor {
	return m.monitor
}

// Init loads all gateways from SQLite and starts their monitors.
// Must be called after db.Init().
func (m *Manager) Init() error {
	gateways, err := m.GetGateways()
	if err != nil {
		return fmt.Errorf("gateway manager init: %w", err)
	}
	for _, gw := range gateways {
		m.monitor.Start(gw)
	}
	log.Printf("gateway-manager: init complete (%d gateways)", len(gateways))
	return nil
}

// ── Gateways ──────────────────────────────────────────────────────────────────

// GetGateways returns all gateways ordered by created_at.
func (m *Manager) GetGateways() ([]Gateway, error) {
	rows, err := db.DB().Query(`
		SELECT id, name, interface, gateway_ip, monitor_address,
		       enabled, monitor, monitor_interval, window_seconds,
		       latency_threshold, monitor_http, monitor_rule,
		       description, created_at
		FROM gateways ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Gateway
	for rows.Next() {
		gw, err := scanGateway(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, gw)
	}
	return out, rows.Err()
}

// GetGateway returns a single gateway by ID, or nil if not found.
func (m *Manager) GetGateway(id string) (*Gateway, error) {
	row := db.DB().QueryRow(`
		SELECT id, name, interface, gateway_ip, monitor_address,
		       enabled, monitor, monitor_interval, window_seconds,
		       latency_threshold, monitor_http, monitor_rule,
		       description, created_at
		FROM gateways WHERE id = ?
	`, id)
	gw, err := scanGatewayRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return gw, err
}

// GatewayInput is the create/update request payload.
type GatewayInput struct {
	Name             string            `json:"name"`
	Interface        string            `json:"interface"`
	GatewayIP        string            `json:"gatewayIP"`
	MonitorAddress   string            `json:"monitorAddress"`
	Enabled          *bool             `json:"enabled"`
	Monitor          *bool             `json:"monitor"`
	MonitorInterval  int               `json:"monitorInterval"`
	WindowSeconds    int               `json:"windowSeconds"`
	LatencyThreshold int               `json:"latencyThreshold"`
	MonitorHttp      MonitorHttpConfig `json:"monitorHttp"`
	MonitorRule      string            `json:"monitorRule"`
	Description      string            `json:"description"`
}

// CreateGateway persists a new gateway and starts its monitor.
func (m *Manager) CreateGateway(inp GatewayInput) (*Gateway, error) {
	if err := validateGatewayInput(inp); err != nil {
		return nil, err
	}

	gw := gatewayFromInput(inp)
	gw.ID = uuid.New().String()
	gw.CreatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := insertGateway(gw); err != nil {
		return nil, err
	}

	m.monitor.Start(gw)
	log.Printf("gateway-manager: created gateway %q (%s)", gw.Name, gw.ID)
	return &gw, nil
}

// UpdateGateway replaces gateway fields, persists, and restarts its monitor.
func (m *Manager) UpdateGateway(id string, inp GatewayInput) (*Gateway, error) {
	existing, err := m.GetGateway(id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("gateway not found")
	}
	if err := validateGatewayInput(inp); err != nil {
		return nil, err
	}

	gw := gatewayFromInput(inp)
	gw.ID = existing.ID
	gw.CreatedAt = existing.CreatedAt

	if err := updateGateway(gw); err != nil {
		return nil, err
	}

	// Restart monitor with updated parameters (resets all probe windows).
	m.monitor.Start(gw)
	log.Printf("gateway-manager: updated gateway %q (%s)", gw.Name, gw.ID)
	return &gw, nil
}

// DeleteGateway stops the monitor, removes the gateway from SQLite.
func (m *Manager) DeleteGateway(id string) error {
	gw, err := m.GetGateway(id)
	if err != nil {
		return err
	}
	if gw == nil {
		return fmt.Errorf("gateway not found")
	}

	m.monitor.Stop(id)

	if _, err := db.DB().Exec(`DELETE FROM gateways WHERE id = ?`, id); err != nil {
		return err
	}

	log.Printf("gateway-manager: deleted gateway %q (%s)", gw.Name, id)
	return nil
}

// GetGatewayWithStatus combines gateway data with live monitoring status.
func (m *Manager) GetGatewayWithStatus(id string) (*GatewayWithStatus, error) {
	gw, err := m.GetGateway(id)
	if err != nil || gw == nil {
		return nil, err
	}
	return &GatewayWithStatus{Gateway: *gw, MonitorStatus: m.monitor.GetStatus(id)}, nil
}

// GetAllGatewaysWithStatus returns all gateways enriched with live status.
func (m *Manager) GetAllGatewaysWithStatus() ([]GatewayWithStatus, error) {
	gateways, err := m.GetGateways()
	if err != nil {
		return nil, err
	}
	out := make([]GatewayWithStatus, len(gateways))
	for i, gw := range gateways {
		out[i] = GatewayWithStatus{Gateway: gw, MonitorStatus: m.monitor.GetStatus(gw.ID)}
	}
	return out, nil
}

// ── Gateway Groups ────────────────────────────────────────────────────────────

// GetGroups returns all gateway groups ordered by created_at.
func (m *Manager) GetGroups() ([]GatewayGroup, error) {
	rows, err := db.DB().Query(`
		SELECT id, name, trigger, description, members, created_at
		FROM gateway_groups ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GatewayGroup
	for rows.Next() {
		grp, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, grp)
	}
	return out, rows.Err()
}

// GetGroup returns a single group by ID, or nil if not found.
func (m *Manager) GetGroup(id string) (*GatewayGroup, error) {
	row := db.DB().QueryRow(`
		SELECT id, name, trigger, description, members, created_at
		FROM gateway_groups WHERE id = ?
	`, id)
	grp, err := scanGroupRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return grp, err
}

// GatewayGroupInput is the create/update request payload.
type GatewayGroupInput struct {
	Name        string               `json:"name"`
	Trigger     string               `json:"trigger"`
	Description string               `json:"description"`
	Gateways    []GatewayGroupMember `json:"gateways"`
}

// CreateGroup persists a new gateway group.
func (m *Manager) CreateGroup(inp GatewayGroupInput) (*GatewayGroup, error) {
	if err := validateGroupInput(inp); err != nil {
		return nil, err
	}

	membersJSON, _ := json.Marshal(inp.Gateways)
	grp := GatewayGroup{
		ID:          uuid.New().String(),
		Name:        strings.TrimSpace(inp.Name),
		Trigger:     inp.Trigger,
		Description: strings.TrimSpace(inp.Description),
		Gateways:    inp.Gateways,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	_, err := db.DB().Exec(`
		INSERT INTO gateway_groups (id, name, trigger, description, members, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, grp.ID, grp.Name, grp.Trigger, grp.Description, string(membersJSON), grp.CreatedAt)
	if err != nil {
		return nil, err
	}

	log.Printf("gateway-manager: created group %q (%s)", grp.Name, grp.ID)
	return &grp, nil
}

// UpdateGroup replaces group fields and persists.
func (m *Manager) UpdateGroup(id string, inp GatewayGroupInput) (*GatewayGroup, error) {
	existing, err := m.GetGroup(id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("gateway group not found")
	}
	if err := validateGroupInput(inp); err != nil {
		return nil, err
	}

	members := inp.Gateways
	if members == nil {
		members = []GatewayGroupMember{}
	}
	membersJSON, _ := json.Marshal(members)

	_, err = db.DB().Exec(`
		UPDATE gateway_groups
		SET name = ?, trigger = ?, description = ?, members = ?
		WHERE id = ?
	`, strings.TrimSpace(inp.Name), inp.Trigger, strings.TrimSpace(inp.Description),
		string(membersJSON), id)
	if err != nil {
		return nil, err
	}

	grp := GatewayGroup{
		ID:          existing.ID,
		Name:        strings.TrimSpace(inp.Name),
		Trigger:     inp.Trigger,
		Description: strings.TrimSpace(inp.Description),
		Gateways:    members,
		CreatedAt:   existing.CreatedAt,
	}
	log.Printf("gateway-manager: updated group %q (%s)", grp.Name, id)
	return &grp, nil
}

// DeleteGroup removes a gateway group from SQLite.
func (m *Manager) DeleteGroup(id string) error {
	grp, err := m.GetGroup(id)
	if err != nil {
		return err
	}
	if grp == nil {
		return fmt.Errorf("gateway group not found")
	}

	if _, err := db.DB().Exec(`DELETE FROM gateway_groups WHERE id = ?`, id); err != nil {
		return err
	}

	log.Printf("gateway-manager: deleted group %q (%s)", grp.Name, id)
	return nil
}

// ── Private: DB helpers ───────────────────────────────────────────────────────

type gatewayScanner interface {
	Scan(dest ...any) error
}

func scanGateway(rows *sql.Rows) (Gateway, error) {
	gw, err := scanGatewayRow(rows)
	if err != nil || gw == nil {
		return Gateway{}, err
	}
	return *gw, nil
}

func scanGatewayRow(s gatewayScanner) (*Gateway, error) {
	var gw Gateway
	var enabled, monitor int
	var monitorHttpJSON string

	err := s.Scan(
		&gw.ID, &gw.Name, &gw.Interface, &gw.GatewayIP, &gw.MonitorAddress,
		&enabled, &monitor, &gw.MonitorInterval, &gw.WindowSeconds,
		&gw.LatencyThreshold, &monitorHttpJSON, &gw.MonitorRule,
		&gw.Description, &gw.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	gw.Enabled = enabled != 0
	gw.Monitor = monitor != 0

	if monitorHttpJSON != "" && monitorHttpJSON != "{}" {
		_ = json.Unmarshal([]byte(monitorHttpJSON), &gw.MonitorHttp)
	}

	// Apply defaults for zero values (mirrors Gateway.js constructor defaults).
	if gw.MonitorInterval == 0 {
		gw.MonitorInterval = 5
	}
	if gw.LatencyThreshold == 0 {
		gw.LatencyThreshold = 500
	}
	if gw.MonitorRule == "" {
		gw.MonitorRule = "icmp_only"
	}
	if gw.MonitorHttp.ExpectedStatus == 0 {
		gw.MonitorHttp.ExpectedStatus = 200
	}
	if gw.MonitorHttp.Interval == 0 {
		gw.MonitorHttp.Interval = 10
	}
	if gw.MonitorHttp.Timeout == 0 {
		gw.MonitorHttp.Timeout = 5
	}

	return &gw, nil
}

func insertGateway(gw Gateway) error {
	httpJSON, _ := json.Marshal(gw.MonitorHttp)
	_, err := db.DB().Exec(`
		INSERT INTO gateways
		    (id, name, interface, gateway_ip, monitor_address,
		     enabled, monitor, monitor_interval, window_seconds,
		     latency_threshold, monitor_http, monitor_rule,
		     description, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		gw.ID, gw.Name, gw.Interface, gw.GatewayIP, gw.MonitorAddress,
		boolInt(gw.Enabled), boolInt(gw.Monitor), gw.MonitorInterval, gw.WindowSeconds,
		gw.LatencyThreshold, string(httpJSON), gw.MonitorRule,
		gw.Description, gw.CreatedAt,
	)
	return err
}

func updateGateway(gw Gateway) error {
	httpJSON, _ := json.Marshal(gw.MonitorHttp)
	_, err := db.DB().Exec(`
		UPDATE gateways
		SET name = ?, interface = ?, gateway_ip = ?, monitor_address = ?,
		    enabled = ?, monitor = ?, monitor_interval = ?, window_seconds = ?,
		    latency_threshold = ?, monitor_http = ?, monitor_rule = ?,
		    description = ?
		WHERE id = ?
	`,
		gw.Name, gw.Interface, gw.GatewayIP, gw.MonitorAddress,
		boolInt(gw.Enabled), boolInt(gw.Monitor), gw.MonitorInterval, gw.WindowSeconds,
		gw.LatencyThreshold, string(httpJSON), gw.MonitorRule,
		gw.Description, gw.ID,
	)
	return err
}

type groupScanner interface {
	Scan(dest ...any) error
}

func scanGroup(rows *sql.Rows) (GatewayGroup, error) {
	grp, err := scanGroupRow(rows)
	if err != nil {
		return GatewayGroup{}, err
	}
	return *grp, nil
}

func scanGroupRow(s groupScanner) (*GatewayGroup, error) {
	var grp GatewayGroup
	var membersJSON string

	err := s.Scan(&grp.ID, &grp.Name, &grp.Trigger, &grp.Description, &membersJSON, &grp.CreatedAt)
	if err != nil {
		return nil, err
	}

	if membersJSON != "" && membersJSON != "[]" {
		_ = json.Unmarshal([]byte(membersJSON), &grp.Gateways)
	}
	if grp.Gateways == nil {
		grp.Gateways = []GatewayGroupMember{}
	}
	if grp.Trigger == "" {
		grp.Trigger = "packetloss"
	}

	return &grp, nil
}

// ── Private: input helpers ────────────────────────────────────────────────────

// gatewayFromInput maps a GatewayInput to a Gateway with defaults applied.
func gatewayFromInput(inp GatewayInput) Gateway {
	enabled := true
	if inp.Enabled != nil {
		enabled = *inp.Enabled
	}
	monitor := true
	if inp.Monitor != nil {
		monitor = *inp.Monitor
	}

	interval := inp.MonitorInterval
	if interval <= 0 {
		interval = 5
	}
	threshold := inp.LatencyThreshold
	if threshold <= 0 {
		threshold = 500
	}
	rule := inp.MonitorRule
	if rule == "" {
		rule = "icmp_only"
	}

	http := inp.MonitorHttp
	if http.ExpectedStatus == 0 {
		http.ExpectedStatus = 200
	}
	if http.Interval == 0 {
		http.Interval = 10
	}
	if http.Timeout == 0 {
		http.Timeout = 5
	}

	return Gateway{
		Name:             strings.TrimSpace(inp.Name),
		Interface:        strings.TrimSpace(inp.Interface),
		GatewayIP:        strings.TrimSpace(inp.GatewayIP),
		MonitorAddress:   strings.TrimSpace(inp.MonitorAddress),
		Enabled:          enabled,
		Monitor:          monitor,
		MonitorInterval:  interval,
		WindowSeconds:    inp.WindowSeconds,
		LatencyThreshold: threshold,
		MonitorHttp:      http,
		MonitorRule:      rule,
		Description:      strings.TrimSpace(inp.Description),
	}
}

func validateGatewayInput(inp GatewayInput) error {
	if strings.TrimSpace(inp.Name) == "" {
		return fmt.Errorf("gateway name is required")
	}
	if strings.TrimSpace(inp.Interface) == "" {
		return fmt.Errorf("interface is required")
	}
	if strings.TrimSpace(inp.GatewayIP) == "" {
		return fmt.Errorf("gatewayIP is required")
	}
	rule := inp.MonitorRule
	if rule != "" && rule != "icmp_only" && rule != "http_only" && rule != "all" && rule != "any" {
		return fmt.Errorf("monitorRule must be icmp_only, http_only, all, or any")
	}
	return nil
}

func validateGroupInput(inp GatewayGroupInput) error {
	if strings.TrimSpace(inp.Name) == "" {
		return fmt.Errorf("group name is required")
	}
	trigger := inp.Trigger
	if trigger != "" && trigger != "packetloss" && trigger != "latency" && trigger != "packetloss_latency" {
		return fmt.Errorf("trigger must be packetloss, latency, or packetloss_latency")
	}
	return nil
}

// boolInt converts bool to 0/1 for SQLite storage.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ── Singleton accessor ────────────────────────────────────────────────────────

var gwInstance *Manager

// SetInstance stores the initialized Manager for package-level access.
// Must be called from main() before serving requests.
func SetInstance(m *Manager) { gwInstance = m }

// Get returns the package-level Manager singleton.
// Panics with a clear message if SetInstance was not called (programming error).
func Get() *Manager {
	if gwInstance == nil {
		panic("gateway: manager not initialized — call SetInstance before Get()")
	}
	return gwInstance
}

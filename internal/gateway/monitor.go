package gateway

// Monitor manages per-gateway ICMP and HTTP probe goroutines with sliding windows.
//
// Design (replaces JS EventEmitter):
//   - Each gateway gets one ICMP goroutine and optionally one HTTP goroutine.
//   - Goroutines are stopped by closing a per-gateway stopCh channel.
//   - Status changes are broadcast to registered StatusChangeFunc callbacks
//     (analogous to EventEmitter.emit('statusChange', ...)).
//   - Status is computed from sliding windows: probes older than windowSeconds
//     are evicted on each new probe.
//   - At least minProbes probes must exist before committing to healthy/degraded/down.
//   - HTTP window is auto-expanded so it always contains at least minProbes probes
//     (avoids "unknown" forever when HTTP interval > windowSeconds).
//
// Concurrency:
//   - Monitor.mu (RWMutex) guards the states map and handlers slice.
//   - Each monitorState has its own mu (Mutex) for probe data and status.
//   - Probe goroutines write to state.mu; GetStatus readers also lock state.mu.

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JohnnyVBut/cascade/internal/settings"
	"github.com/JohnnyVBut/cascade/internal/util"
	"github.com/JohnnyVBut/cascade/internal/validate"
)

const minProbes = 3 // minimum window probes before committing to a non-unknown status

// ── Internal types ────────────────────────────────────────────────────────────

// probe is a single measurement stored in a sliding window.
type probe struct {
	ts      int64 // Unix milliseconds
	success bool
	latency *int  // ms; nil if probe failed
}

// windowStats summarises a slice of probes.
type windowStats struct {
	total       int
	successRate float64 // 0–100
	packetLoss  *int    // %; nil if total == 0
	avgLatency  *int    // ms; nil if no successful probes
}

// monitorState holds mutable per-gateway monitoring data.
// Protected by its own mu to allow concurrent reads from GetStatus.
type monitorState struct {
	mu         sync.Mutex
	icmpProbes []probe
	httpProbes []probe
	status     MonitorStatus
	stopCh     chan struct{}
}

// StatusChangeFunc is invoked when a gateway's combined status changes.
// Called synchronously from a probe goroutine — must not block.
type StatusChangeFunc func(gatewayID, newStatus, prevStatus string)

// ── Monitor ───────────────────────────────────────────────────────────────────

// Monitor manages probe lifecycles and status for all gateways.
type Monitor struct {
	mu       sync.RWMutex
	states   map[string]*monitorState
	handlers []StatusChangeFunc
}

// NewMonitor creates a Monitor ready for use.
func NewMonitor() *Monitor {
	return &Monitor{
		states: make(map[string]*monitorState),
	}
}

// OnStatusChange registers fn to be called on every gateway status transition.
// May be called multiple times; all handlers are invoked in registration order.
func (m *Monitor) OnStatusChange(fn StatusChangeFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = append(m.handlers, fn)
}

// Start launches probe goroutines for gw.
// If monitoring is already running, it is stopped and restarted (windows reset).
// A copy of gw is captured at call time — later gateway updates require a new Start call.
func (m *Monitor) Start(gw Gateway) {
	m.Stop(gw.ID)

	if !gw.Enabled || !gw.Monitor {
		log.Printf("gateway-monitor: %s (%s): monitoring disabled, skipping", gw.ID, gw.Name)
		return
	}

	state := &monitorState{
		stopCh: make(chan struct{}),
		status: MonitorStatus{Status: "unknown"},
	}

	m.mu.Lock()
	m.states[gw.ID] = state
	m.mu.Unlock()

	// ICMP goroutine: fires immediately then every monitorInterval seconds.
	icmpInterval := time.Duration(gw.MonitorInterval) * time.Second
	if icmpInterval < time.Second {
		icmpInterval = 5 * time.Second
	}
	go func() {
		m.probeICMP(gw, state)
		ticker := time.NewTicker(icmpInterval)
		defer ticker.Stop()
		for {
			select {
			case <-state.stopCh:
				return
			case <-ticker.C:
				m.probeICMP(gw, state)
			}
		}
	}()

	// HTTP goroutine: started only when monitorRule requires HTTP and URL is set.
	httpNeeded := gw.MonitorRule != "icmp_only"
	if httpNeeded && gw.MonitorHttp.URL != "" {
		httpInterval := time.Duration(gw.MonitorHttp.Interval) * time.Second
		if httpInterval < time.Second {
			httpInterval = 10 * time.Second
		}
		go func() {
			m.probeHTTP(gw, state)
			ticker := time.NewTicker(httpInterval)
			defer ticker.Stop()
			for {
				select {
				case <-state.stopCh:
					return
				case <-ticker.C:
					m.probeHTTP(gw, state)
				}
			}
		}()
	}

	log.Printf("gateway-monitor: %s (%s): started (ICMP every %ds)", gw.ID, gw.Name, gw.MonitorInterval)
}

// Stop halts all probe goroutines for gatewayID and clears its state.
func (m *Monitor) Stop(gatewayID string) {
	m.mu.Lock()
	state, ok := m.states[gatewayID]
	if ok {
		close(state.stopCh)
		delete(m.states, gatewayID)
	}
	m.mu.Unlock()

	if ok {
		log.Printf("gateway-monitor: %s: stopped", gatewayID)
	}
}

// GetStatus returns the current live status of a gateway.
// Returns MonitorStatus{Status:"unknown"} if the gateway is not monitored.
func (m *Monitor) GetStatus(gatewayID string) MonitorStatus {
	m.mu.RLock()
	state, ok := m.states[gatewayID]
	m.mu.RUnlock()

	if !ok {
		return MonitorStatus{Status: "unknown"}
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	return state.status
}

// ── ICMP probe ────────────────────────────────────────────────────────────────

func (m *Monitor) probeICMP(gw Gateway, state *monitorState) {
	target := gw.MonitorAddress
	if target == "" {
		target = gw.GatewayIP
	}

	// Defence-in-depth: validate before shell interpolation (HIGH-6).
	// Protects against legacy rows written before input validation was added.
	if err := validate.IfaceName(gw.Interface); err != nil {
		log.Printf("gateway-monitor: %s: skipping ICMP probe — unsafe interface %q: %v", gw.ID, gw.Interface, err)
		return
	}
	if err := validate.HostOrIP(target); err != nil {
		log.Printf("gateway-monitor: %s: skipping ICMP probe — unsafe target %q: %v", gw.ID, target, err)
		return
	}

	var success bool
	var latency *int

	cmd := fmt.Sprintf("ping -c 1 -W 1 -I %s %s", gw.Interface, target)
	out, err := util.Exec(cmd, 5*time.Second, false)
	if err == nil {
		// Parse packet loss: "0% packet loss" → success; "100% packet loss" → fail.
		lossStr := reFind(out, `(\d+)% packet loss`, 1)
		loss := 100
		if lossStr != "" {
			if n, e := strconv.Atoi(lossStr); e == nil {
				loss = n
			}
		}
		success = loss < 100

		if success {
			// RTT: Linux  "rtt min/avg/max/mdev = X/AVG/X/X ms"
			//      Alpine "round-trip min/avg/max = X/AVG/X ms"
			avgStr := reFind(out, `(?:rtt|round-trip)[^\n]+=\s*[\d.]+/([\d.]+)/`, 1)
			if avgStr != "" {
				var f float64
				fmt.Sscanf(avgStr, "%f", &f)
				v := int(math.Round(f))
				latency = &v
			}
		}
	}

	windowSec, thHealthy, thDegraded := globalThresholds(gw.WindowSeconds)

	state.mu.Lock()
	addToWindow(&state.icmpProbes, probe{ts: nowMs(), success: success, latency: latency}, windowSec)
	prevStatus := state.status.Status
	recomputeStatus(gw, state, windowSec, thHealthy, thDegraded)
	newStatus := state.status.Status
	state.mu.Unlock()

	if newStatus != prevStatus {
		m.emitChange(gw.ID, newStatus, prevStatus)
	}
}

// ── HTTP probe ────────────────────────────────────────────────────────────────

func (m *Monitor) probeHTTP(gw Gateway, state *monitorState) {
	h := gw.MonitorHttp
	timeoutDur := time.Duration(h.Timeout) * time.Second
	if timeoutDur <= 0 {
		timeoutDur = 5 * time.Second
	}
	expected := h.ExpectedStatus
	if expected == 0 {
		expected = 200
	}

	var success bool
	var latencyMs *int
	var httpCode *int

	// Use native Go HTTP client with TLS verification disabled (monitoring probe).
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402
	}
	client := &http.Client{Timeout: timeoutDur, Transport: transport}

	ctx, cancel := context.WithTimeout(context.Background(), timeoutDur)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err == nil {
		req.Header.Set("User-Agent", "cascade-monitor/3.0")
		req.Header.Set("Connection", "close")

		start := time.Now()
		resp, rerr := client.Do(req)
		if rerr == nil {
			ms := int(time.Since(start).Milliseconds())
			code := resp.StatusCode
			latencyMs = &ms
			httpCode = &code
			success = resp.StatusCode == expected
			resp.Body.Close()
			log.Printf("gateway-monitor: %s: HTTP %d (expected %d) %dms", gw.ID, resp.StatusCode, expected, ms)
		} else {
			log.Printf("gateway-monitor: %s: HTTP probe failed: %v", gw.ID, rerr)
		}
	}

	windowSec, thHealthy, thDegraded := globalThresholds(gw.WindowSeconds)

	// HTTP window is auto-expanded to hold at least minProbes at the configured interval.
	httpIntervalSec := h.Interval
	if httpIntervalSec <= 0 {
		httpIntervalSec = 10
	}
	httpWindowSec := windowSec
	if minWindow := httpIntervalSec * (minProbes + 1); minWindow > httpWindowSec {
		httpWindowSec = minWindow
	}

	p := probe{ts: nowMs(), success: success, latency: latencyMs}
	if !success {
		p.latency = nil // only record latency for successful probes
	}

	now := time.Now().UTC().Format(time.RFC3339)

	state.mu.Lock()
	addToWindow(&state.httpProbes, p, httpWindowSec)
	// Update HTTP-specific fields before recomputing combined status.
	state.status.HttpLastCheck = &now
	state.status.HttpCode = httpCode
	prevStatus := state.status.Status
	recomputeStatus(gw, state, windowSec, thHealthy, thDegraded)
	newStatus := state.status.Status
	state.mu.Unlock()

	if newStatus != prevStatus {
		m.emitChange(gw.ID, newStatus, prevStatus)
	}
}

// ── Status computation ────────────────────────────────────────────────────────

// recomputeStatus recalculates the combined status from both windows.
// Must be called with state.mu held.
func recomputeStatus(gw Gateway, state *monitorState, windowSec int, thHealthy, thDegraded float64) {
	icmpSt := calcWindowStats(state.icmpProbes)
	httpSt := calcWindowStats(state.httpProbes)

	icmpStatus := statusFromRate(icmpSt.total, icmpSt.successRate, thHealthy, thDegraded)

	httpEnabled := gw.MonitorRule != "icmp_only" && gw.MonitorHttp.URL != ""
	var httpStatusPtr *string
	if httpEnabled {
		s := statusFromRate(httpSt.total, httpSt.successRate, thHealthy, thDegraded)
		httpStatusPtr = &s
	}

	combined := combineStatuses(gw.MonitorRule, icmpStatus, httpStatusPtr)
	now := time.Now().UTC().Format(time.RFC3339)

	// Preserve HttpLastCheck and HttpCode — they are updated directly in probeHTTP.
	state.status.Status = combined
	state.status.Latency = icmpSt.avgLatency
	state.status.PacketLoss = icmpSt.packetLoss
	state.status.LastCheck = &now
	state.status.HttpStatus = httpStatusPtr
	state.status.HttpLatency = httpSt.avgLatency
}

// combineStatuses applies monitorRule to ICMP and HTTP individual statuses.
func combineStatuses(rule, icmpStatus string, httpStatus *string) string {
	isGood := func(s string) bool { return s == "healthy" || s == "degraded" }

	switch rule {
	case "http_only":
		if httpStatus == nil {
			return "unknown"
		}
		return *httpStatus

	case "all":
		if httpStatus == nil {
			return icmpStatus
		}
		if icmpStatus == "unknown" || *httpStatus == "unknown" {
			return "unknown"
		}
		if icmpStatus == "healthy" && *httpStatus == "healthy" {
			return "healthy"
		}
		if isGood(icmpStatus) && isGood(*httpStatus) {
			return "degraded"
		}
		return "down"

	case "any":
		if httpStatus == nil || *httpStatus == "unknown" {
			return icmpStatus
		}
		if icmpStatus == "unknown" && *httpStatus == "unknown" {
			return "unknown"
		}
		if isGood(icmpStatus) || isGood(*httpStatus) {
			if icmpStatus == "healthy" || *httpStatus == "healthy" {
				return "healthy"
			}
			return "degraded"
		}
		return "down"

	default: // "icmp_only"
		return icmpStatus
	}
}

// ── Window math ───────────────────────────────────────────────────────────────

// addToWindow appends p and evicts probes older than windowSeconds.
func addToWindow(probes *[]probe, p probe, windowSeconds int) {
	*probes = append(*probes, p)
	cutoff := nowMs() - int64(windowSeconds)*1000
	i := 0
	for i < len(*probes) && (*probes)[i].ts < cutoff {
		i++
	}
	if i > 0 {
		*probes = (*probes)[i:]
	}
}

// calcWindowStats computes aggregate metrics over a probe slice.
func calcWindowStats(probes []probe) windowStats {
	total := len(probes)
	if total == 0 {
		return windowStats{}
	}

	lost := 0
	for _, p := range probes {
		if !p.success {
			lost++
		}
	}

	rate := float64(total-lost) / float64(total) * 100
	loss := lost * 100 / total
	st := windowStats{total: total, successRate: rate, packetLoss: &loss}

	var sumMs int64
	var good int
	for _, p := range probes {
		if p.success && p.latency != nil {
			sumMs += int64(*p.latency)
			good++
		}
	}
	if good > 0 {
		avg := int(sumMs / int64(good))
		st.avgLatency = &avg
	}
	return st
}

// statusFromRate converts a success rate into a status string.
func statusFromRate(total int, rate, thHealthy, thDegraded float64) string {
	if total < minProbes {
		return "unknown"
	}
	if rate >= thHealthy {
		return "healthy"
	}
	if rate >= thDegraded {
		return "degraded"
	}
	return "down"
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// globalThresholds reads current gateway thresholds from Settings.
// If gwWindowSeconds is non-zero, it overrides the global window.
// Falls back to hardcoded defaults if Settings.GetSettings() fails.
func globalThresholds(gwWindowSeconds int) (windowSec int, healthy, degraded float64) {
	s, err := settings.GetSettings()
	if err != nil {
		// Fallback to hardcoded defaults (mirrors Settings.js DEFAULTS).
		windowSec = 30
		healthy = 95
		degraded = 90
	} else {
		windowSec = s.GatewayWindowSeconds
		healthy = s.GatewayHealthyThreshold
		degraded = s.GatewayDegradedThreshold
	}
	if gwWindowSeconds > 0 {
		windowSec = gwWindowSeconds
	}
	return
}

// emitChange calls all registered StatusChangeFunc handlers.
func (m *Monitor) emitChange(gatewayID, newStatus, prevStatus string) {
	m.mu.RLock()
	handlers := m.handlers
	m.mu.RUnlock()

	for _, h := range handlers {
		h(gatewayID, newStatus, prevStatus)
	}
}

// nowMs returns current Unix time in milliseconds.
func nowMs() int64 {
	return time.Now().UnixMilli()
}

// reFind returns capture group n from the first match of pattern in s.
// Returns "" if no match or group is empty.
var reFindCache sync.Map // pattern → *regexp.Regexp

func reFind(s, pattern string, group int) string {
	var re *regexp.Regexp
	if v, ok := reFindCache.Load(pattern); ok {
		re = v.(*regexp.Regexp)
	} else {
		re = regexp.MustCompile(pattern)
		reFindCache.Store(pattern, re)
	}
	m := re.FindStringSubmatch(s)
	if len(m) > group {
		return strings.TrimSpace(m[group])
	}
	return ""
}

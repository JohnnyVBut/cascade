// Package gateway manages gateways, gateway groups, and their live monitoring.
// Mirrors Gateway.js, GatewayGroup.js, GatewayMonitor.js, and GatewayManager.js
// from the Node.js version.
//
// Storage: SQLite tables `gateways` and `gateway_groups`.
// Monitoring: per-gateway goroutine pairs (ICMP + HTTP) with sliding windows.
package gateway

// MonitorHttpConfig holds HTTP probe parameters for a gateway.
// Stored as JSON in the `monitor_http` column.
type MonitorHttpConfig struct {
	Enabled        bool   `json:"enabled"`
	URL            string `json:"url"`
	ExpectedStatus int    `json:"expectedStatus"` // 0 → 200
	Interval       int    `json:"interval"`       // seconds, 0 → 10
	Timeout        int    `json:"timeout"`        // seconds, 0 → 5
}

// Gateway is a monitored next-hop or upstream endpoint.
// Mirrors the Gateway.js model.
type Gateway struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Interface        string            `json:"interface"`        // outbound host interface (eth0, wg10, …)
	GatewayIP        string            `json:"gatewayIP"`        // next-hop IP for routing
	MonitorAddress   string            `json:"monitorAddress"`   // ICMP target; '' → use GatewayIP
	Enabled          bool              `json:"enabled"`
	Monitor          bool              `json:"monitor"`
	MonitorInterval  int               `json:"monitorInterval"`  // ICMP probe interval, seconds
	WindowSeconds    int               `json:"windowSeconds"`    // sliding window; 0 → use global default
	LatencyThreshold int               `json:"latencyThreshold"` // ms, for display only
	MonitorHttp      MonitorHttpConfig `json:"monitorHttp"`
	MonitorRule      string            `json:"monitorRule"` // icmp_only | http_only | all | any
	Description      string            `json:"description"`
	CreatedAt        string            `json:"createdAt"`
}

// GatewayGroupMember is one entry inside a GatewayGroup.
// Tier 1 = highest priority. Weight is used for load balancing within the same tier.
type GatewayGroupMember struct {
	GatewayID string `json:"gatewayId"`
	Tier      int    `json:"tier"`
	Weight    int    `json:"weight"`
}

// GatewayGroup aggregates gateways with tier-based failover.
// Mirrors GatewayGroup.js.
type GatewayGroup struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Trigger     string               `json:"trigger"` // packetloss | latency | packetloss_latency
	Description string               `json:"description"`
	Gateways    []GatewayGroupMember `json:"gateways"` // stored as JSON in DB column `members`
	CreatedAt   string               `json:"createdAt"`
}

// MonitorStatus is the live probe state of a gateway returned by Monitor.GetStatus.
type MonitorStatus struct {
	Status        string  `json:"status"`        // unknown | healthy | degraded | down
	Latency       *int    `json:"latency"`       // ICMP avg latency ms; nil if no data
	PacketLoss    *int    `json:"packetLoss"`    // ICMP loss %; nil if no data
	LastCheck     *string `json:"lastCheck"`     // ISO8601
	HttpStatus    *string `json:"httpStatus"`    // nil if HTTP not configured
	HttpLatency   *int    `json:"httpLatency"`   // nil if no HTTP data
	HttpLastCheck *string `json:"httpLastCheck"` // nil if no HTTP probe yet
	HttpCode      *int    `json:"httpCode"`      // last HTTP response code; nil if no probe yet
}

// GatewayWithStatus combines Gateway data with its live monitoring status.
// This is what the API returns.
type GatewayWithStatus struct {
	Gateway
	MonitorStatus
}

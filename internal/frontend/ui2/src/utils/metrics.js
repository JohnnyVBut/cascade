// Diagnostics metric definitions: which keys are available from a /metrics
// snapshot, how to extract their value, their label and semantic color.
//
// Key formats:
//   'cpu'            — CPU load percent
//   'mem'            — memory used percent
//   'net:<ifaceId>'  — interface traffic, both directions on one chart
//                       (ifaceId = system/kernel name, e.g. "wg10")

// Build the list of available metric keys from a snapshot + interface list.
// net.* in the snapshot and metrics_history are keyed by the interface's
// SYSTEM name (iface.id, e.g. "wg10" — from /proc/net/dev), not the display
// name the user picked when creating it, so lookups must match on iface.id.
// The label still shows both, since the display name is what the user
// actually recognizes at a glance.
export function availableMetrics(snapshot, interfaces, gateways) {
  const list = [
    { key: 'cpu', label: 'CPU %', group: 'System' },
    { key: 'mem', label: 'RAM %', group: 'System' },
  ]
  const net = (snapshot && snapshot.net) || {}
  for (const iface of (interfaces || [])) {
    if (!(iface.id in net)) continue
    const label = `${iface.name} (${iface.id})`
    list.push({ key: `net:${iface.id}`, label: `${label} traffic`, group: label })
  }
  for (const gw of (gateways || [])) {
    list.push({ key: `gateway:${gw.id}`, label: `${gw.name} status`, group: 'Gateways' })
  }
  return list
}

// True if the key is a gateway status metric (rendered as a stacked bar chart).
export function isGatewayKey(key) {
  return key.startsWith('gateway:')
}

// True if the key is an interface traffic metric (rendered as a two-line
// rx/tx chart via netValues() below, rather than a single metricValue()).
export function isNetKey(key) {
  return key.startsWith('net:')
}

// Extract a numeric value for a key from a snapshot; null if unavailable.
// Gateway values are a status code: >=3 healthy, 2 degraded, 1 down, <=0 admin/unknown.
// Not used for net: keys — see netValues(), which returns both directions.
export function metricValue(snapshot, key) {
  if (!snapshot) return null
  if (key === 'cpu') return snapshot.cpu ?? null
  if (key === 'mem') return snapshot.mem ?? null
  if (key.startsWith('gateway:')) {
    const id = key.slice(8)
    return (snapshot.gateways && snapshot.gateways[id]) ?? null
  }
  return null
}

// { rx, tx } Mbps for an interface traffic key; { rx: null, tx: null } if
// the interface isn't reporting in this snapshot.
export function netValues(snapshot, key) {
  const id = key.slice(4)
  const ns = snapshot && snapshot.net && snapshot.net[id]
  return { rx: ns ? ns.rxMbps ?? null : null, tx: ns ? ns.txMbps ?? null : null }
}

// Stacked gateway status colors (healthy → admin_down), fixed hex for both themes.
export const GATEWAY_STACK_COLORS = {
  healthy: '#22c55e',
  degraded: '#eab308',
  down: '#ef4444',
  adminDown: '#9ca3af',
}

// Semantic color CSS var for a key's TEXT (adapts to theme via tokens).
export function metricColor(key) {
  return '#38bdf8'
}

// Fixed hex for the CHART stroke/fill (uPlot canvas can't read CSS vars).
// Chosen to read acceptably on both light and dark backgrounds.
export function metricChartColor(key) {
  return '#0ea5e9'
}

// rx/tx stroke colors for a net: chart — must match --danger-fg/--success-fg
// exactly (the colors used for the ↓/↑ current-value text) since uPlot's
// canvas can't read CSS vars, so the values are mirrored here per theme.
const NET_COLORS = {
  light: { rx: '#dc2626', tx: '#15803d' },
  dark: { rx: '#f87171', tx: '#4ade80' },
}
export function netChartColors(theme) {
  return NET_COLORS[theme] || NET_COLORS.light
}

// Health percent → token color, using the same admin-configured thresholds
// as the Gateway Monitor settings (gatewayHealthyThreshold/
// gatewayDegradedThreshold — Settings → Gateway Monitor). Inverse of
// usageColor's severity direction (here HIGH is good).
// healthyAt/degradedAt default to the backend's defaults (95/90) so the UI
// degrades gracefully before global settings have loaded.
export function healthColor(pct, healthyAt = 95, degradedAt = 90) {
  if (pct == null) return 'var(--text-muted)'
  if (pct >= healthyAt) return 'var(--success-fg)'
  if (pct >= degradedAt) return 'var(--warning-fg)'
  return 'var(--danger-fg)'
}

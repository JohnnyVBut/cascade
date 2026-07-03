// Diagnostics metric definitions: which keys are available from a /metrics
// snapshot, how to extract their value, their label and semantic color.
//
// Key formats:
//   'cpu'                 — CPU load percent
//   'mem'                 — memory used percent
//   'net:<ifaceName>:rx'  — interface download Mbps
//   'net:<ifaceName>:tx'  — interface upload Mbps

// Build the list of available metric keys from a snapshot + interface list.
// Interfaces provide stable names/labels; net.* keys in the snapshot confirm
// which are actually reporting.
export function availableMetrics(snapshot, interfaces) {
  const list = [
    { key: 'cpu', label: 'CPU %', group: 'System' },
    { key: 'mem', label: 'RAM %', group: 'System' },
  ]
  const net = (snapshot && snapshot.net) || {}
  for (const iface of (interfaces || [])) {
    if (!(iface.name in net)) continue
    list.push({ key: `net:${iface.name}:rx`, label: `${iface.name} ↓ Mbps`, group: iface.name })
    list.push({ key: `net:${iface.name}:tx`, label: `${iface.name} ↑ Mbps`, group: iface.name })
  }
  return list
}

// Extract a numeric value for a key from a snapshot; null if unavailable.
export function metricValue(snapshot, key) {
  if (!snapshot) return null
  if (key === 'cpu') return snapshot.cpu ?? null
  if (key === 'mem') return snapshot.mem ?? null
  if (key.startsWith('net:')) {
    const [, name, dir] = key.split(':')
    const ns = snapshot.net && snapshot.net[name]
    if (!ns) return null
    return (dir === 'rx' ? ns.rxMbps : ns.txMbps) ?? null
  }
  return null
}

// Human label for a key (falls back to the key itself).
export function metricLabel(key, interfaces) {
  if (key === 'cpu') return 'CPU %'
  if (key === 'mem') return 'RAM %'
  if (key.startsWith('net:')) {
    const [, name, dir] = key.split(':')
    return `${name} ${dir === 'rx' ? '↓' : '↑'} Mbps`
  }
  return key
}

// Semantic color CSS var for a key's TEXT (adapts to theme via tokens):
// download red, upload green, system blue.
export function metricColor(key) {
  if (key.endsWith(':rx')) return 'var(--danger-fg)'
  if (key.endsWith(':tx')) return 'var(--success-fg)'
  return '#38bdf8'
}

// Fixed hex for the CHART stroke/fill (uPlot canvas can't read CSS vars).
// Chosen to read acceptably on both light and dark backgrounds.
export function metricChartColor(key) {
  if (key.endsWith(':rx')) return '#ef4444'
  if (key.endsWith(':tx')) return '#22c55e'
  return '#0ea5e9'
}

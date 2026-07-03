// Formatting helpers. All rounded to avoid float artifacts.

// Mbps → human string (Mbps under 1000, else Gbps).
export function fmtRate(mbps) {
  const v = Number(mbps) || 0
  if (v >= 1000) return { value: (v / 1000).toFixed(1), unit: 'Gbps' }
  if (v >= 10) return { value: Math.round(v).toString(), unit: 'Mbps' }
  return { value: v.toFixed(1), unit: 'Mbps' }
}

// Percent 0..100 → "42%".
export function fmtPct(p) {
  return `${Math.round(Number(p) || 0)}%`
}

// KB (from /proc/meminfo) → GB string.
export function fmtMemGB(kb) {
  return ((Number(kb) || 0) / 1024 / 1024).toFixed(1)
}

// Bytes → GB string (for disk, which the API reports in raw bytes).
export function fmtBytesGB(bytes) {
  return ((Number(bytes) || 0) / 1024 / 1024 / 1024).toFixed(1)
}

// Usage percent → a token color, gradated by severity (used for capacity
// bars: memory, disk). <70% ok, 70-89% warning, >=90% danger.
export function usageColor(pct) {
  const p = Number(pct) || 0
  if (p >= 90) return 'var(--danger)'
  if (p >= 70) return 'var(--warning)'
  return 'var(--accent)'
}

// Bytes → compact "1.2G" / "88M" / "512K" / "0".
export function fmtBytes(bytes) {
  const b = Number(bytes) || 0
  if (b >= 1e9) return (b / 1e9).toFixed(1) + 'G'
  if (b >= 1e6) return Math.round(b / 1e6) + 'M'
  if (b >= 1e3) return Math.round(b / 1e3) + 'K'
  return String(b)
}

// ISO timestamp → relative "2m ago" / "1h ago" / "3d ago" / "never".
export function fmtAgo(iso) {
  if (!iso) return 'never'
  const t = new Date(iso).getTime()
  if (!t) return 'never'
  const s = Math.max(0, Math.round((Date.now() - t) / 1000))
  if (s < 60) return s + 's ago'
  const m = Math.round(s / 60)
  if (m < 60) return m + 'm ago'
  const h = Math.round(m / 60)
  if (h < 24) return h + 'h ago'
  return Math.round(h / 24) + 'd ago'
}

// Derive up to two uppercase initials from a name.
export function initials(name) {
  const parts = String(name || '').trim().split(/[\s\-_.]+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}

// kbps → "1.5M" / "512K". Empty string when zero/unset.
export function fmtKbps(kbps) {
  const v = Number(kbps) || 0
  if (v <= 0) return ''
  if (v >= 1000) return (v / 1000).toFixed(v % 1000 === 0 ? 0 : 1) + 'M'
  return v + 'K'
}

// Resolve effective rate limit for a peer: its own limit takes precedence,
// otherwise falls back to its client group's limit. Mirrors the legacy
// peerEffectiveRate() logic in internal/frontend/www/js/app.js.
export function effectiveRate(peer, groups) {
  if (peer.rateDown > 0 || peer.rateUp > 0) {
    return { rateDown: peer.rateDown, rateUp: peer.rateUp, fromGroup: false }
  }
  if (peer.groupId) {
    const g = (groups || []).find(g => g.id === peer.groupId)
    if (g && (g.rateDown > 0 || g.rateUp > 0)) {
      return { rateDown: g.rateDown, rateUp: g.rateUp, fromGroup: true }
    }
  }
  return { rateDown: 0, rateUp: 0, fromGroup: false }
}

// Expiry urgency: 'expired' | 'soon' (within 7 days) | 'far' | null (no expiry).
export function expiryUrgency(expiredAt) {
  if (!expiredAt) return null
  const t = new Date(expiredAt).getTime()
  if (!t) return null
  const diff = t - Date.now()
  if (diff < 0) return 'expired'
  if (diff < 7 * 24 * 3600 * 1000) return 'soon'
  return 'far'
}

// Short date "DD-MM-YYYY" for expiry badges.
export function fmtDateShort(iso) {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleDateString('en-GB', { day: '2-digit', month: '2-digit', year: 'numeric' }).replace(/\//g, '-')
}

// Gateway status → design token CSS var (single source of truth, mirrors
// gatewayStatusColor() in internal/frontend/www/js/app.js).
const GATEWAY_STATUS_VAR = {
  healthy: 'var(--success-fg)',
  degraded: 'var(--warning-fg)',
  down: 'var(--danger-fg)',
  admin_down: 'var(--idle-fg)',
  unknown: 'var(--idle-fg)',
}
export function gatewayStatusColor(status) {
  return GATEWAY_STATUS_VAR[status] || GATEWAY_STATUS_VAR.unknown
}

// Extract the IP from a peer's runtimeEndpoint ("1.2.3.4:51820" or
// "[::1]:51820") — the real public address seen in the last handshake, as
// opposed to the peer's internal VPN address. '' if no endpoint yet.
export function peerPublicIP(endpoint) {
  if (!endpoint) return ''
  if (endpoint.startsWith('[')) return endpoint.slice(1, endpoint.indexOf(']'))
  return endpoint.split(':')[0]
}

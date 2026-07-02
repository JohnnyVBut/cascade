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

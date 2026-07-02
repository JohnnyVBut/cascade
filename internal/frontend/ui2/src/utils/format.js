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

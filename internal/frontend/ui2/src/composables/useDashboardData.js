import { api } from '../api/client.js'
import { usePolling } from './usePolling.js'

// System info (hostname, uptime, load, memory) — changes slowly, poll 5s.
export function useSystemInfo() {
  return usePolling(() => api.get('/dashboard/system-info'), 5000, {})
}

// Live metrics (cpu, mem, per-interface rx/tx Mbps, gateway statuses) — 2s.
export function useMetrics() {
  return usePolling(() => api.get('/metrics'), 2000, {})
}

// Tunnel interfaces. Wrapped as { interfaces: [...] } by the backend.
export function useInterfaces() {
  return usePolling(async () => {
    const res = await api.get('/tunnel-interfaces')
    return res.interfaces || []
  }, 5000, [])
}

// Gateways with live monitoring status. Wrapped as { gateways: [...] }.
export function useGateways() {
  return usePolling(async () => {
    const res = await api.get('/gateways')
    return res.gateways || []
  }, 5000, [])
}

// Client groups (id, name, rateDown/rateUp in kbps). Rarely change, poll slowly.
export function useClientGroups() {
  return usePolling(async () => {
    const res = await api.get('/aliases/client-groups')
    return res.groups || []
  }, 30000, [])
}

// Historical metric series for a longer period. Returns { points: [[ts, val], ...] }.
export function fetchMetricsHistory(key, period) {
  return api.get(`/metrics/history?key=${encodeURIComponent(key)}&period=${period}`)
}

// Gateway status distribution per bucket for the stacked chart.
// Returns { buckets: [[ts_ms, healthy, degraded, down, admin_down], ...] }.
export function fetchGatewayDist(key, period) {
  return api.get(`/metrics/gateway-dist?key=${encodeURIComponent(key)}&period=${period}`)
}

// Interface actions. Caller refreshes afterwards.
export function startInterface(id) {
  return api.post(`/tunnel-interfaces/${id}/start`)
}
export function stopInterface(id) {
  return api.post(`/tunnel-interfaces/${id}/stop`)
}
export function restartInterface(id) {
  return api.post(`/tunnel-interfaces/${id}/restart`)
}

// Peer actions.
export function enablePeer(ifaceId, peerId) {
  return api.post(`/tunnel-interfaces/${ifaceId}/peers/${peerId}/enable`)
}
export function disablePeer(ifaceId, peerId) {
  return api.post(`/tunnel-interfaces/${ifaceId}/peers/${peerId}/disable`)
}
export function deletePeer(ifaceId, peerId) {
  return api.delete(`/tunnel-interfaces/${ifaceId}/peers/${peerId}`)
}

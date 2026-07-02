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

// Start or stop an interface, then the caller should refresh.
export function startInterface(id) {
  return api.post(`/tunnel-interfaces/${id}/start`)
}
export function stopInterface(id) {
  return api.post(`/tunnel-interfaces/${id}/stop`)
}

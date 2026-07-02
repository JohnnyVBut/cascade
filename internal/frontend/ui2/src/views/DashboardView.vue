<script setup>
import { computed } from 'vue'
import StatCard from '../components/StatCard.vue'
import BaseCard from '../components/BaseCard.vue'
import StatusBadge from '../components/StatusBadge.vue'
import BaseToggle from '../components/BaseToggle.vue'
import {
  useSystemInfo, useMetrics, useInterfaces, useGateways,
  startInterface, stopInterface,
} from '../composables/useDashboardData.js'
import { fmtRate, fmtPct, fmtMemGB } from '../utils/format.js'

const { data: sys } = useSystemInfo()
const { data: metrics } = useMetrics()
const { data: interfaces, refresh: refreshInterfaces } = useInterfaces()
const { data: gateways } = useGateways()

const netMap = computed(() => (metrics.value && metrics.value.net) || {})

const upCount = computed(() => interfaces.value.filter(i => i.enabled).length)
const totalPeers = computed(() => interfaces.value.reduce((n, i) => n + (i.peerCount || 0), 0))

const totalRate = computed(() => {
  let sum = 0
  for (const k in netMap.value) {
    const ns = netMap.value[k]
    sum += (ns.rxMbps || 0) + (ns.txMbps || 0)
  }
  return fmtRate(sum)
})

function ifaceRate(name) {
  const ns = netMap.value[name]
  if (!ns) return null
  return { rx: fmtRate(ns.rxMbps || 0), tx: fmtRate(ns.txMbps || 0) }
}

const gatewayTone = { healthy: 'success', degraded: 'warning', down: 'danger', admin_down: 'idle', unknown: 'idle' }

async function toggleInterface(iface) {
  try {
    if (iface.enabled) await stopInterface(iface.id)
    else await startInterface(iface.id)
  } catch (e) {
    // Surface later via a toast system; refresh reflects the true state.
  } finally {
    refreshInterfaces()
  }
}
</script>

<template>
  <section style="display:flex; flex-direction:column; gap:22px;">
    <h1 style="font-size:22px; font-weight:500; letter-spacing:-0.01em; margin:0;">Dashboard</h1>

    <!-- Metric row -->
    <div style="display:grid; grid-template-columns:repeat(5,1fr); gap:12px;">
      <StatCard label="Interfaces" :value="upCount" :suffix="'/ ' + interfaces.length + ' up'" />
      <StatCard label="Peers" :value="totalPeers" />
      <StatCard label="Throughput" :value="totalRate.value" :suffix="totalRate.unit" />
      <StatCard label="CPU" :value="fmtPct(metrics.cpu)" />
      <StatCard label="Memory" :value="fmtPct(metrics.mem)" />
    </div>

    <!-- Interfaces -->
    <div>
      <div style="font-size:13px; color:var(--text-muted); margin-bottom:10px;">Interfaces</div>
      <div v-if="interfaces.length === 0" style="font-size:14px; color:var(--text-secondary);">
        No interfaces yet.
      </div>
      <div v-else style="display:grid; grid-template-columns:repeat(auto-fill, minmax(250px, 1fr)); gap:12px;">
        <BaseCard v-for="iface in interfaces" :key="iface.id">
          <div style="display:flex; align-items:center; gap:8px; margin-bottom:10px;">
            <span
              :style="{ width:'8px', height:'8px', borderRadius:'50%', flexShrink:0,
                        background: iface.enabled ? 'var(--success)' : 'var(--idle)' }"
            />
            <span style="font-size:14px; font-weight:500; flex:1; min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">{{ iface.name }}</span>
            <BaseToggle :model-value="iface.enabled" @update:model-value="toggleInterface(iface)" />
          </div>
          <div style="font-size:12px; color:var(--text-muted); font-family:ui-monospace,monospace; margin-bottom:8px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">{{ iface.address }}</div>
          <div style="font-size:12px; color:var(--text-secondary); margin-bottom:10px;">
            {{ iface.protocol === 'amneziawg-2.0' ? 'AmneziaWG' : 'WireGuard' }} · :{{ iface.listenPort }} · {{ iface.peerCount || 0 }} peers
          </div>
          <div style="display:flex; align-items:center; justify-content:space-between; gap:8px;">
            <span v-if="ifaceRate(iface.name)" style="font-size:12px; color:var(--text-secondary); font-family:ui-monospace,monospace;">
              ↓{{ ifaceRate(iface.name).rx.value }} ↑{{ ifaceRate(iface.name).tx.value }}
            </span>
            <span v-else style="font-size:12px; color:var(--text-muted);">—</span>
            <StatusBadge :tone="iface.enabled ? 'success' : 'idle'" :label="iface.enabled ? 'up' : 'down'" />
          </div>
        </BaseCard>
      </div>
    </div>

    <!-- Gateways (only when configured) -->
    <div v-if="gateways.length > 0">
      <div style="font-size:13px; color:var(--text-muted); margin-bottom:10px;">Gateways</div>
      <div style="display:grid; grid-template-columns:repeat(auto-fill, minmax(250px, 1fr)); gap:12px;">
        <BaseCard v-for="gw in gateways" :key="gw.id">
          <div style="display:flex; align-items:center; gap:8px; margin-bottom:10px;">
            <span style="font-size:14px; font-weight:500; flex:1; min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">{{ gw.name }}</span>
            <StatusBadge :tone="gatewayTone[gw.status] || 'idle'" :label="gw.status || 'unknown'" />
          </div>
          <div style="font-size:12px; color:var(--text-muted); margin-bottom:8px;">{{ gw.interface }}</div>
          <div style="font-size:12px; color:var(--text-secondary); font-family:ui-monospace,monospace;">
            <span v-if="gw.latency != null">{{ gw.latency }} ms</span><span v-else>— ms</span>
            · <span v-if="gw.packetLoss != null">{{ gw.packetLoss }}% loss</span><span v-else>—% loss</span>
          </div>
        </BaseCard>
      </div>
    </div>

    <!-- System panel -->
    <div>
      <div style="font-size:13px; color:var(--text-muted); margin-bottom:10px;">System</div>
      <BaseCard>
        <div style="display:grid; grid-template-columns:repeat(3,1fr); gap:16px;">
          <div>
            <div style="font-size:12px; color:var(--text-muted); margin-bottom:4px;">Host</div>
            <div style="font-size:14px; font-weight:500;">{{ sys.hostname || '—' }}</div>
            <div style="font-size:12px; color:var(--text-secondary); margin-top:2px;">up {{ sys.uptime || '—' }}</div>
          </div>
          <div>
            <div style="font-size:12px; color:var(--text-muted); margin-bottom:4px;">Load average</div>
            <div style="font-size:14px; font-weight:500; font-family:ui-monospace,monospace;">
              {{ (sys.load1 || 0).toFixed(2) }} · {{ (sys.load5 || 0).toFixed(2) }} · {{ (sys.load15 || 0).toFixed(2) }}
            </div>
          </div>
          <div>
            <div style="font-size:12px; color:var(--text-muted); margin-bottom:4px;">Memory</div>
            <div style="font-size:14px; font-weight:500;">
              {{ fmtMemGB(sys.memUsed) }} / {{ fmtMemGB(sys.memTotal) }} GB
            </div>
            <div style="height:5px; border-radius:3px; background:var(--border); margin-top:6px; overflow:hidden;">
              <div :style="{ width: (sys.memPct || 0) + '%', height:'100%', background:'var(--accent)' }" />
            </div>
          </div>
        </div>
      </BaseCard>
    </div>
  </section>
</template>

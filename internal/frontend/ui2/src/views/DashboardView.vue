<script setup>
import { computed } from 'vue'
import { IconServer2, IconRoute, IconUsers } from '@tabler/icons-vue'
import WidgetPanel from '../components/WidgetPanel.vue'
import StatusBadge from '../components/StatusBadge.vue'
import InterfaceCard from '../components/InterfaceCard.vue'
import {
  useSystemInfo, useMetrics, useInterfaces, useGateways,
} from '../composables/useDashboardData.js'
import { fmtPct, fmtMemGB } from '../utils/format.js'

const { data: sys } = useSystemInfo()
const { data: metrics } = useMetrics()
const { data: interfaces, refresh: refreshInterfaces } = useInterfaces()
const { data: gateways } = useGateways()

const netMap = computed(() => (metrics.value && metrics.value.net) || {})
function ifaceRate(name) { return netMap.value[name] || null }

const totalPeers = computed(() => interfaces.value.reduce((n, i) => n + ((i.peers && i.peers.length) || 0), 0))
const peerDist = computed(() =>
  interfaces.value.map(i => ({ name: i.name, count: (i.peers && i.peers.length) || 0 }))
    .filter(x => x.count > 0)
)
const healthyGw = computed(() => gateways.value.filter(g => g.status === 'healthy').length)
const gatewayTone = { healthy: 'success', degraded: 'warning', down: 'danger', admin_down: 'idle', unknown: 'idle' }
const distColors = ['var(--accent)', 'var(--success)', 'var(--warning)', '#a78bfa', '#f472b6']
</script>

<template>
  <section style="display:flex; flex-direction:column; gap:22px;">
    <h1 style="font-size:22px; font-weight:500; letter-spacing:-0.01em; margin:0;">Dashboard</h1>

    <!-- Monitoring widgets -->
    <div style="display:grid; grid-template-columns:repeat(auto-fit, minmax(260px, 1fr)); gap:14px;">

      <WidgetPanel title="System" icon-color="#a78bfa">
        <template #icon><IconServer2 :size="16" /></template>
        <template #summary><span style="font-family:ui-monospace,monospace;">{{ sys.hostname || '—' }}</span></template>
        <div style="display:flex; flex-direction:column; gap:11px;">
          <div>
            <div style="display:flex; justify-content:space-between; font-size:12px; margin-bottom:4px;">
              <span style="color:var(--text-muted);">CPU</span>
              <span style="font-family:ui-monospace,monospace;">{{ fmtPct(metrics.cpu) }}</span>
            </div>
            <div class="bar"><div class="bar-fill" :style="{ width: (metrics.cpu || 0) + '%' }" /></div>
          </div>
          <div>
            <div style="display:flex; justify-content:space-between; font-size:12px; margin-bottom:4px;">
              <span style="color:var(--text-muted);">Memory</span>
              <span style="font-family:ui-monospace,monospace;">{{ fmtMemGB(sys.memUsed) }} / {{ fmtMemGB(sys.memTotal) }} GB</span>
            </div>
            <div class="bar"><div class="bar-fill" :style="{ width: (sys.memPct || 0) + '%' }" /></div>
          </div>
          <div style="display:flex; justify-content:space-between; font-size:12px;">
            <span style="color:var(--text-muted);">Load</span>
            <span style="font-family:ui-monospace,monospace;">{{ (sys.load1||0).toFixed(2) }} · {{ (sys.load5||0).toFixed(2) }} · {{ (sys.load15||0).toFixed(2) }}</span>
          </div>
          <div style="display:flex; justify-content:space-between; font-size:12px;">
            <span style="color:var(--text-muted);">Uptime</span>
            <span style="font-family:ui-monospace,monospace;">{{ sys.uptime || '—' }}</span>
          </div>
        </div>
      </WidgetPanel>

      <WidgetPanel v-if="gateways.length > 0" title="Gateways" icon-color="var(--warning)">
        <template #icon><IconRoute :size="16" /></template>
        <template #summary>{{ healthyGw }} / {{ gateways.length }} healthy</template>
        <div style="display:flex; flex-direction:column;">
          <div v-for="(gw, idx) in gateways" :key="gw.id"
               style="display:flex; align-items:center; gap:8px; padding:7px 0;"
               :style="{ borderTop: idx > 0 ? '1px solid var(--border)' : 'none' }">
            <span class="dot" :style="{ background: gatewayTone[gw.status] === 'success' ? 'var(--success)' : gatewayTone[gw.status] === 'warning' ? 'var(--warning)' : gatewayTone[gw.status] === 'danger' ? 'var(--danger)' : 'var(--idle)' }" />
            <span style="font-size:13px; font-weight:500;">{{ gw.name }}</span>
            <span style="font-size:12px; color:var(--text-muted);">{{ gw.interface }}</span>
            <span style="margin-left:auto; font-size:12px; color:var(--text-secondary); font-family:ui-monospace,monospace;">
              <template v-if="gw.latency != null">{{ gw.latency }}ms · {{ gw.packetLoss != null ? gw.packetLoss : '—' }}%</template>
              <template v-else>—</template>
            </span>
          </div>
        </div>
      </WidgetPanel>

      <WidgetPanel title="Peers" icon-color="#f472b6">
        <template #icon><IconUsers :size="16" /></template>
        <div style="display:flex; align-items:baseline; gap:8px;">
          <span style="font-size:30px; font-weight:500;">{{ totalPeers }}</span>
          <span style="font-size:12px; color:var(--text-muted);">across {{ interfaces.length }} interfaces</span>
        </div>
        <div v-if="peerDist.length > 0" style="margin-top:14px; display:flex; gap:4px;">
          <div v-for="(d, idx) in peerDist" :key="d.name"
               :style="{ flex: d.count, height:'6px', borderRadius:'3px', background: distColors[idx % distColors.length] }" />
        </div>
        <div v-if="peerDist.length > 0" style="margin-top:6px; font-size:11px; color:var(--text-muted);">
          {{ peerDist.map(d => d.name + ' ' + d.count).join(' · ') }}
        </div>
      </WidgetPanel>

    </div>

    <!-- Interfaces (full management) -->
    <div>
      <div style="font-size:13px; color:var(--text-muted); margin-bottom:10px;">Interfaces</div>
      <div v-if="interfaces.length === 0" style="font-size:14px; color:var(--text-secondary);">
        No interfaces yet.
      </div>
      <div v-else style="display:grid; grid-template-columns:repeat(auto-fill, minmax(440px, 1fr)); gap:12px; align-items:start;">
        <InterfaceCard
          v-for="iface in interfaces" :key="iface.id"
          :iface="iface" :rate="ifaceRate(iface.name)"
          @changed="refreshInterfaces"
        />
      </div>
    </div>
  </section>
</template>

<style scoped>
.bar { height: 4px; border-radius: 3px; background: var(--border); overflow: hidden; }
.bar-fill { height: 100%; background: var(--accent); }
.dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
</style>

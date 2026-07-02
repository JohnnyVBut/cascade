<script setup>
import { computed } from 'vue'
import { IconServer2, IconRoute } from '@tabler/icons-vue'
import WidgetPanel from '../components/WidgetPanel.vue'
import InterfaceCompactRow from '../components/InterfaceCompactRow.vue'
import PeersPanel from '../components/PeersPanel.vue'
import {
  useSystemInfo, useMetrics, useInterfaces, useGateways,
} from '../composables/useDashboardData.js'
import { fmtPct, fmtMemGB } from '../utils/format.js'
import { useToast } from '../composables/useToast.js'

const { data: sys } = useSystemInfo()
const { data: metrics } = useMetrics()
const { data: interfaces, refresh: refreshInterfaces } = useInterfaces()
const { data: gateways } = useGateways()
const { push } = useToast()

const netMap = computed(() => (metrics.value && metrics.value.net) || {})
function ifaceRate(name) { return netMap.value[name] || null }

const upCount = computed(() => interfaces.value.filter(i => i.enabled).length)
const totalPeers = computed(() => interfaces.value.reduce((n, i) => n + ((i.peers && i.peers.length) || 0), 0))
const healthyGw = computed(() => gateways.value.filter(g => g.status === 'healthy').length)
const gatewayTone = { healthy: 'success', degraded: 'warning', down: 'danger', admin_down: 'idle', unknown: 'idle' }

function onAddPeer() { push('Coming soon', 'warning') }
</script>

<template>
  <section style="display:flex; flex-direction:column; gap:22px;">
    <div style="display:flex; align-items:baseline; gap:16px;">
      <h1 style="font-size:22px; font-weight:500; letter-spacing:-0.01em; margin:0;">Dashboard</h1>
      <span style="font-size:12px; color:var(--text-muted);">
        {{ upCount }}/{{ interfaces.length }} interfaces up · {{ totalPeers }} peers · CPU {{ fmtPct(metrics.cpu) }} · Mem {{ fmtPct(metrics.mem) }}
      </span>
    </div>

    <!-- Monitoring widgets -->
    <div style="display:grid; grid-template-columns:repeat(auto-fit, minmax(280px, 1fr)); gap:14px;">

      <WidgetPanel title="System" icon-color="#a78bfa">
        <template #icon><IconServer2 :size="16" /></template>
        <template #summary><span style="font-family:ui-monospace,monospace;">{{ sys.hostname || '—' }}</span></template>
        <div style="display:flex; gap:20px;">
          <div style="flex:1;">
            <div style="display:flex; justify-content:space-between; font-size:12px; margin-bottom:4px;">
              <span style="color:var(--text-muted);">CPU</span>
              <span style="font-family:ui-monospace,monospace;">{{ fmtPct(metrics.cpu) }}</span>
            </div>
            <div class="bar"><div class="bar-fill" :style="{ width: (metrics.cpu || 0) + '%' }" /></div>
          </div>
          <div style="flex:1;">
            <div style="display:flex; justify-content:space-between; font-size:12px; margin-bottom:4px;">
              <span style="color:var(--text-muted);">Memory</span>
              <span style="font-family:ui-monospace,monospace;">{{ fmtMemGB(sys.memUsed) }}/{{ fmtMemGB(sys.memTotal) }}G</span>
            </div>
            <div class="bar"><div class="bar-fill" :style="{ width: (sys.memPct || 0) + '%' }" /></div>
          </div>
        </div>
        <div style="display:flex; justify-content:space-between; font-size:12px; margin-top:10px;">
          <span style="color:var(--text-muted);">Load {{ (sys.load1||0).toFixed(2) }} · {{ (sys.load5||0).toFixed(2) }} · {{ (sys.load15||0).toFixed(2) }}</span>
          <span style="color:var(--text-muted); font-family:ui-monospace,monospace;">up {{ sys.uptime || '—' }}</span>
        </div>
      </WidgetPanel>

      <WidgetPanel v-if="gateways.length > 0" title="Gateways" icon-color="var(--warning)">
        <template #icon><IconRoute :size="16" /></template>
        <template #summary>{{ healthyGw }} / {{ gateways.length }} healthy</template>
        <div style="display:flex; flex-direction:column;">
          <div v-for="(gw, idx) in gateways" :key="gw.id"
               style="display:flex; align-items:center; gap:8px; padding:6px 0;"
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

    </div>

    <!-- Interfaces (left, compact controls) + Peers (right, filterable list) -->
    <div class="split">
      <div style="display:flex; flex-direction:column; gap:10px;">
        <div style="font-size:13px; color:var(--text-muted);">Interfaces</div>
        <div v-if="interfaces.length === 0" style="font-size:14px; color:var(--text-secondary);">
          No interfaces yet.
        </div>
        <InterfaceCompactRow
          v-for="iface in interfaces" :key="iface.id"
          :iface="iface" :rate="ifaceRate(iface.name)"
          @changed="refreshInterfaces"
          @add-peer="onAddPeer"
        />
      </div>

      <PeersPanel :interfaces="interfaces" @changed="refreshInterfaces" />
    </div>
  </section>
</template>

<style scoped>
.bar { height: 4px; border-radius: 3px; background: var(--border); overflow: hidden; }
.bar-fill { height: 100%; background: var(--accent); }
.dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
.split { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; align-items: start; }
@media (max-width: 900px) {
  .split { grid-template-columns: 1fr; }
}
</style>

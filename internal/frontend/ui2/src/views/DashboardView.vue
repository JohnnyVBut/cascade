<script setup>
import { computed } from 'vue'
import { IconServer2, IconRoute } from '@tabler/icons-vue'
import WidgetPanel from '../components/WidgetPanel.vue'
import InterfacesPanel from '../components/InterfacesPanel.vue'
import PeersPanel from '../components/PeersPanel.vue'
import GridPlaceholder from '../components/GridPlaceholder.vue'
import {
  useSystemInfo, useMetrics, useInterfaces, useGateways,
} from '../composables/useDashboardData.js'
import { fmtPct, fmtMemGB, gatewayStatusColor } from '../utils/format.js'
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

    <!--
      4-column widget grid. Rows are composed EXPLICITLY (no reliance on grid
      auto-flow wrap): each row's spans are padded to exactly 4/4 with
      GridPlaceholder, so layout stays predictable even when a widget is
      conditionally hidden (e.g. no gateways configured).
      Row 1: System 1/4, Gateways 1/4, Interfaces 1/2
      Row 2: Peers 1/2, (placeholder) 1/2
    -->
    <div class="grid4">

      <WidgetPanel class="g-1" title="System" icon-color="#a78bfa">
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

      <WidgetPanel v-if="gateways.length > 0" class="g-1" title="Gateways" icon-color="var(--warning)">
        <template #icon><IconRoute :size="16" /></template>
        <template #summary>{{ healthyGw }} / {{ gateways.length }} healthy</template>
        <div style="display:flex; flex-direction:column;">
          <div v-for="(gw, idx) in gateways" :key="gw.id"
               style="display:flex; align-items:center; gap:8px; padding:6px 0;"
               :style="{ borderTop: idx > 0 ? '1px solid var(--border)' : 'none' }">
            <span class="dot" :style="{ background: gatewayStatusColor(gw.status) }" />
            <span style="font-size:13px; font-weight:500; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">{{ gw.name }}</span>
            <span style="margin-left:auto; font-size:12px; font-family:ui-monospace,monospace; flex-shrink:0;" :style="{ color: gatewayStatusColor(gw.status) }">
              <template v-if="gw.latency != null">{{ gw.latency }}ms</template>
              <template v-else>—</template>
            </span>
          </div>
        </div>
      </WidgetPanel>
      <GridPlaceholder v-else :span="1" />

      <InterfacesPanel
        class="g-2"
        :interfaces="interfaces" :rate-for="ifaceRate"
        @changed="refreshInterfaces" @add-peer="onAddPeer"
      />
      <PeersPanel class="g-2" :interfaces="interfaces" @changed="refreshInterfaces" />
      <GridPlaceholder :span="2" />
    </div>
  </section>
</template>

<style scoped>
.bar { height: 4px; border-radius: 3px; background: var(--border); overflow: hidden; }
.bar-fill { height: 100%; background: var(--accent); }
.dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
</style>

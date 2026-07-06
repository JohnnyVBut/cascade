<script setup>
import { computed } from 'vue'
import { IconServer2, IconRoute } from '@tabler/icons-vue'
import WidgetPanel from '../components/WidgetPanel.vue'
import InterfacesPanel from '../components/InterfacesPanel.vue'
import PeersPanel from '../components/PeersPanel.vue'
import DiagnosticsPanel from '../components/DiagnosticsPanel.vue'
import GridPlaceholder from '../components/GridPlaceholder.vue'
import {
  useSystemInfo, useMetrics, useInterfaces, useGateways,
} from '../composables/useDashboardData.js'
import { fmtPct, fmtMemGB, fmtBytesGB, fmtRate, usageColor, gatewayStatusColor } from '../utils/format.js'
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

function truncName(name, max = 15) {
  name = name || ''
  return name.length > max ? name.slice(0, max) + '…' : name
}

// "4 cores @ 2.50GHz Intel(R) Xeon..." shown next to the CPU label; hidden
// entirely if the backend couldn't determine core count (cpuCores defaults
// to 0). The clock speed is pulled out of the model string (which reports it
// as a trailing "@ X.XXGHz") and moved to the front, ahead of the vendor text.
function splitCpuModel(model) {
  const match = model.match(/@\s*([\d.]+\s*[GM]Hz)/i)
  if (!match) return { freq: '', rest: model }
  return { freq: match[1].replace(/\s+/, ''), rest: (model.slice(0, match.index) + model.slice(match.index + match[0].length)).trim() }
}
const cpuInfo = computed(() => {
  if (!sys.value.cpuCores) return ''
  const cores = `${sys.value.cpuCores} core${sys.value.cpuCores === 1 ? '' : 's'}`
  if (!sys.value.cpuModel) return cores
  const { freq, rest } = splitCpuModel(sys.value.cpuModel)
  return freq ? `${cores} @ ${freq} ${rest}` : `${cores} · ${sys.value.cpuModel}`
})
const cpuInfoShort = computed(() => {
  if (!sys.value.cpuModel) return cpuInfo.value
  const cores = `${sys.value.cpuCores} core${sys.value.cpuCores === 1 ? '' : 's'}`
  const { freq, rest } = splitCpuModel(sys.value.cpuModel)
  const prefix = freq ? `${cores} @ ${freq}` : cores
  return `${prefix} ${truncName(rest, 20)}`
})

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
      Row 1: System 1/4, Gateways 1/4, Diagnostics 1/2
      Row 2: Interfaces 1/2, Peers 1/2
    -->
    <div class="grid4">

      <WidgetPanel class="g-1" title="System" icon-color="#a78bfa">
        <template #icon><IconServer2 :size="16" /></template>
        <template #summary><span style="font-family:ui-monospace,monospace;">{{ sys.hostname || '—' }}</span></template>
        <div style="display:flex; flex-direction:column; gap:11px;">
          <div>
            <div style="display:flex; align-items:baseline; gap:6px; font-size:12px; margin-bottom:4px;">
              <span style="color:var(--text-muted); flex-shrink:0;">CPU</span>
              <span v-if="cpuInfo" class="name-tip-wrap cpu-info" style="color:var(--text-muted); font-size:11px;">
                {{ cpuInfoShort }}
                <span v-if="cpuInfo !== cpuInfoShort" class="name-tip">{{ cpuInfo }}</span>
              </span>
              <span style="margin-left:auto; font-family:ui-monospace,monospace; flex-shrink:0;">{{ fmtPct(metrics.cpu) }}</span>
            </div>
            <div class="bar"><div class="bar-fill" :style="{ width: (metrics.cpu || 0) + '%', background: usageColor(metrics.cpu) }" /></div>
          </div>
          <div>
            <div style="display:flex; justify-content:space-between; font-size:12px; margin-bottom:4px;">
              <span style="color:var(--text-muted);">Memory</span>
              <span style="font-family:ui-monospace,monospace;">{{ fmtMemGB(sys.memUsed) }}/{{ fmtMemGB(sys.memTotal) }}G</span>
            </div>
            <div class="bar"><div class="bar-fill" :style="{ width: (sys.memPct || 0) + '%', background: usageColor(sys.memPct) }" /></div>
          </div>
          <div>
            <div style="display:flex; justify-content:space-between; font-size:12px; margin-bottom:4px;">
              <span style="color:var(--text-muted);">Disk</span>
              <span style="font-family:ui-monospace,monospace;">{{ fmtBytesGB(sys.diskUsed) }}/{{ fmtBytesGB(sys.diskTotal) }}G</span>
            </div>
            <div class="bar"><div class="bar-fill" :style="{ width: (sys.diskPct || 0) + '%', background: usageColor(sys.diskPct) }" /></div>
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
        <div class="gw-grid">
          <template v-for="(gw, idx) in gateways" :key="gw.id">
            <span class="gw-divider" :style="{ borderTop: idx > 0 ? '1px solid var(--border)' : 'none' }"></span>
            <span class="dot" :style="{ background: gatewayStatusColor(gw.status) }" />
            <span class="gw-name name-tip-wrap">
              {{ truncName(gw.name) }}
              <span v-if="gw.name && gw.name.length > 15" class="name-tip">{{ gw.name }}</span>
            </span>
            <span class="gw-iface">{{ gw.interface }}</span>
            <span class="gw-latency" :style="{ color: gatewayStatusColor(gw.status) }">
              <template v-if="gw.latency != null">{{ gw.latency }}ms</template>
              <template v-else>—</template>
            </span>
            <span class="gw-traffic" style="color:var(--success-fg);">
              <template v-if="ifaceRate(gw.interface)">↑ {{ fmtRate(ifaceRate(gw.interface).txMbps).value }}{{ fmtRate(ifaceRate(gw.interface).txMbps).unit }}</template>
            </span>

            <span></span>
            <span></span>
            <span></span>
            <span></span>
            <span class="gw-traffic" style="color:var(--danger-fg);">
              <template v-if="ifaceRate(gw.interface)">↓ {{ fmtRate(ifaceRate(gw.interface).rxMbps).value }}{{ fmtRate(ifaceRate(gw.interface).rxMbps).unit }}</template>
            </span>
          </template>
        </div>
      </WidgetPanel>
      <GridPlaceholder v-else :span="1" />

      <DiagnosticsPanel class="g-2" :interfaces="interfaces" :gateways="gateways" />

      <InterfacesPanel
        class="g-2"
        :interfaces="interfaces"
        @changed="refreshInterfaces" @add-peer="onAddPeer"
      />
      <PeersPanel class="g-2" :interfaces="interfaces" @changed="refreshInterfaces" />
    </div>
  </section>
</template>

<style scoped>
.bar { height: 4px; border-radius: 3px; background: var(--border); overflow: hidden; }
.bar-fill { height: 100%; background: var(--accent); }
.dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
.gw-grid {
  display: grid;
  grid-template-columns: auto auto 1fr auto auto;
  align-content: start;
  column-gap: 8px; row-gap: 2px;
  align-items: center;
  font-size: 12px; font-family: ui-monospace, monospace;
}
.gw-divider { grid-column: 1 / -1; }
.gw-name { font-family: system-ui, sans-serif; font-size: 13px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cpu-info { display: inline-block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.name-tip-wrap { position: relative; }
.name-tip-wrap:hover { overflow: visible; }
.name-tip {
  display: none;
  position: absolute; left: 0; top: 100%; margin-top: 4px; z-index: 10;
  background: var(--surface); border: 1px solid var(--border);
  color: var(--text-primary); font-weight: 400; font-size: 12px;
  padding: 4px 8px; border-radius: 4px; white-space: nowrap;
}
.name-tip-wrap:hover .name-tip { display: block; }
.gw-iface { color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.gw-latency { text-align: right; white-space: nowrap; }
.gw-traffic { text-align: right; white-space: nowrap; min-width: 6ch; }
</style>

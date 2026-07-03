<script setup>
import { ref, reactive, computed, watch, onBeforeUnmount } from 'vue'
import { IconChartLine, IconAdjustments } from '@tabler/icons-vue'
import MiniChart from './MiniChart.vue'
import GatewayChart from './GatewayChart.vue'
import { useMetrics, fetchMetricsHistory, fetchGatewayDist } from '../composables/useDashboardData.js'
import {
  availableMetrics, metricValue, metricColor, metricChartColor, isGatewayKey,
} from '../utils/metrics.js'

const props = defineProps({
  interfaces: { type: Array, required: true },
  gateways: { type: Array, default: () => [] },
})

const { data: metrics } = useMetrics()

const MAX = 150 // realtime points (~5m at 2s)
const PERIODS = ['5m', '1h', '6h', '24h', '7d', '30d']
const PERIOD_SECONDS = { '5m': 300, '1h': 3600, '6h': 21600, '24h': 86400, '7d': 604800, '30d': 2592000 }

// Per-key data. Area keys use {t:[], v:[]}; gateway keys reuse it in realtime
// (v = status code) and gwBuckets in history mode.
const chartData = reactive({})
const gwBuckets = reactive({}) // key -> [[ts,h,d,dn,ad], ...]

function readLS(key, fallback) {
  try { const v = JSON.parse(localStorage.getItem(key)); if (v != null) return v } catch (_) { /* */ }
  return fallback
}
const selectedKeys = ref(readLS('cascade-ui2-diag-metrics', ['cpu', 'mem']))
const period = ref(readLS('cascade-ui2-diag-period', '5m'))
watch(selectedKeys, v => localStorage.setItem('cascade-ui2-diag-metrics', JSON.stringify(v)), { deep: true })
watch(period, v => localStorage.setItem('cascade-ui2-diag-period', JSON.stringify(v)))

const showConfig = ref(false)

const available = computed(() => availableMetrics(metrics.value, props.interfaces, props.gateways))
const grouped = computed(() => {
  const g = {}
  for (const m of available.value) (g[m.group] || (g[m.group] = [])).push(m)
  return g
})
const shownKeys = computed(() => selectedKeys.value.filter(k => available.value.some(m => m.key === k)))
function labelFor(key) {
  const m = available.value.find(m => m.key === key)
  return m ? m.label : key
}
function unitFor(key) {
  if (key === 'cpu' || key === 'mem') return '%'
  if (key.startsWith('net:')) return ' Mbps'
  return ''
}

function currentValue(key) {
  const v = metricValue(metrics.value, key)
  if (v == null) return '—'
  if (key === 'cpu' || key === 'mem') return `${Math.round(v)}%`
  if (isGatewayKey(key)) {
    const s = Math.round(v)
    return s >= 3 ? 'healthy' : s === 2 ? 'degraded' : s === 1 ? 'down' : 'admin'
  }
  return Math.round(v).toString()
}

// Bars for a gateway key in the current period. Each bar carries its unix
// timestamp (seconds) so the chart's tooltip can show date/time.
function gatewayBars(key) {
  if (period.value === '5m') {
    const cd = chartData[key]
    if (!cd) return []
    return cd.v.map((v, i) => {
      const s = Math.round(v)
      return {
        healthy: s >= 3 ? 100 : 0, degraded: s === 2 ? 100 : 0,
        down: s === 1 ? 100 : 0, adminDown: s <= 0 ? 100 : 0,
        time: cd.t[i],
      }
    })
  }
  const bk = gwBuckets[key] || []
  return bk.map(b => {
    const [ts, h, d, dn, ad] = b
    const total = (h + d + dn + ad) || 1
    return {
      healthy: h / total * 100, degraded: d / total * 100,
      down: dn / total * 100, adminDown: ad / total * 100,
      time: ts / 1000,
    }
  })
}

// Reference timestamps for the axis: the actual times backing the charts
// currently on screen (first shownKeys entry with data), not a separately
// computed "period ago vs now" guess. This keeps the axis in lockstep with
// what a chart tooltip reports for the same point — previously the axis was
// computed once per period change (frozen at that instant) while realtime
// data kept accumulating, so hovering a recent point showed a time the axis
// never displayed.
const referenceTimes = computed(() => {
  for (const key of shownKeys.value) {
    const t = (chartData[key] || {}).t
    if (t && t.length > 0) return t
  }
  // Gateway keys in history mode store buckets separately (gwBuckets), not
  // chartData — check those too (ts_ms there, convert to seconds).
  for (const key of shownKeys.value) {
    const bk = gwBuckets[key]
    if (bk && bk.length > 0) return bk.map(b => b[0] / 1000)
  }
  return null
})

// Time axis labels: 6 evenly spaced points, oldest → now, shown as actual
// clock time (or date for multi-day periods) rather than a relative offset
// — avoids mixing units (e.g. "-1h", "-48m", "-36m").
const axisLabels = computed(() => {
  const showDate = period.value === '7d' || period.value === '30d'
  const fmt = (ms) => {
    const d = new Date(ms)
    return showDate
      ? d.toLocaleDateString('en-GB', { day: '2-digit', month: '2-digit' })
      : d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })
  }
  const ref = referenceTimes.value
  if (ref && ref.length > 1) {
    const first = ref[0] * 1000
    const last = ref[ref.length - 1] * 1000
    return [0, 1, 2, 3, 4, 5].map(i => fmt(first + (last - first) * i / 5))
  }
  // No data yet: fall back to period-relative estimate.
  const total = PERIOD_SECONDS[period.value]
  const now = Date.now()
  return [5, 4, 3, 2, 1, 0].map(i => fmt(now - total * i / 5 * 1000))
})

// ── Realtime (5m) accumulation ──────────────────────────────────────────────
watch(metrics, (snap) => {
  if (period.value !== '5m') return
  if (!snap || Object.keys(snap).length === 0) return
  const t = Date.now() / 1000
  for (const m of available.value) {
    const key = m.key
    const cd = chartData[key] || { t: [], v: [] }
    cd.t = [...cd.t, t].slice(-MAX)
    cd.v = [...cd.v, metricValue(snap, key)].slice(-MAX)
    chartData[key] = cd
  }
})

// ── History (longer periods) fetching ───────────────────────────────────────
let refreshTimer = null

async function loadHistory() {
  if (period.value === '5m') return
  for (const key of shownKeys.value) {
    try {
      if (isGatewayKey(key)) {
        const res = await fetchGatewayDist(key, period.value)
        gwBuckets[key] = res.buckets || []
      } else {
        const res = await fetchMetricsHistory(key, period.value)
        const pts = res.points || []
        chartData[key] = { t: pts.map(p => p[0]), v: pts.map(p => Math.round(p[1] * 100) / 100) }
      }
    } catch (_) { /* non-fatal */ }
  }
}

function setupRefresh() {
  if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null }
  if (period.value !== '5m') refreshTimer = setInterval(loadHistory, 30000)
}

// On period change: clear buffers, then load (history) or let realtime fill.
watch(period, () => {
  for (const k in chartData) delete chartData[k]
  for (const k in gwBuckets) delete gwBuckets[k]
  loadHistory()
  setupRefresh()
})
// Fetch history for a newly selected key when in a history period.
watch(shownKeys, (keys, prev) => {
  if (period.value === '5m') return
  const added = keys.filter(k => !prev.includes(k))
  if (added.length) loadHistory()
})

// Initial load if starting in a history period.
if (period.value !== '5m') { loadHistory(); setupRefresh() }

onBeforeUnmount(() => { if (refreshTimer) clearInterval(refreshTimer) })

function toggleKey(key) {
  const i = selectedKeys.value.indexOf(key)
  if (i >= 0) selectedKeys.value.splice(i, 1)
  else selectedKeys.value.push(key)
}
</script>

<template>
  <div class="panel">
    <div class="head">
      <IconChartLine :size="16" style="color:#38bdf8;" />
      <span style="font-size:13px; font-weight:500;">Diagnostics</span>
      <div class="periods">
        <button v-for="p in PERIODS" :key="p" class="period-btn" :class="{ on: period === p }" @click="period = p">{{ p }}</button>
      </div>
      <button class="icon-btn bordered" style="width:26px; height:26px;" title="Select metrics" @click="showConfig = !showConfig">
        <IconAdjustments :size="15" />
      </button>
    </div>

    <div class="body">
      <div v-if="shownKeys.length === 0" style="padding:24px 0; text-align:center; font-size:12px; color:var(--text-muted);">
        Click the adjust icon to add metrics.
      </div>

      <template v-else>
        <div v-for="key in shownKeys" :key="key + period" style="margin-bottom:10px;">
          <div style="display:flex; justify-content:space-between; font-size:11px; margin-bottom:3px;">
            <span style="color:var(--text-muted);">{{ labelFor(key) }}</span>
            <span style="font-family:ui-monospace,monospace;" :style="{ color: metricColor(key) }">{{ currentValue(key) }}</span>
          </div>
          <GatewayChart v-if="isGatewayKey(key)" :bars="gatewayBars(key)" />
          <MiniChart v-else :times="(chartData[key] || {}).t || []" :values="(chartData[key] || {}).v || []" :color="metricChartColor(key)" :unit="unitFor(key)" />
        </div>

        <div class="time-axis">
          <span v-for="(l, i) in axisLabels" :key="i">{{ l }}</span>
        </div>
      </template>
    </div>

    <div v-if="showConfig" class="config-backdrop" @click.self="showConfig = false">
      <div class="config-pop">
        <div style="font-size:12px; font-weight:500; margin-bottom:10px;">Select metrics</div>
        <div v-for="(items, group) in grouped" :key="group" style="margin-bottom:8px;">
          <div style="font-size:10px; color:var(--text-muted); text-transform:uppercase; letter-spacing:0.04em; margin-bottom:3px;">{{ group }}</div>
          <label v-for="m in items" :key="m.key" class="cfg-row">
            <span class="cbox" :class="{ on: selectedKeys.includes(m.key) }"><span v-if="selectedKeys.includes(m.key)">✓</span></span>
            <input type="checkbox" :checked="selectedKeys.includes(m.key)" @change="toggleKey(m.key)" style="display:none;" />
            {{ m.label }}
          </label>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.panel {
  position: relative;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 16px;
  height: 100%;
  box-sizing: border-box;
}
.head { display: flex; align-items: center; gap: 8px; margin-bottom: 14px; }
.periods { margin-left: auto; display: flex; gap: 2px; }
.period-btn {
  font-size: 10.5px; padding: 3px 7px; border-radius: 5px;
  border: none; background: transparent; color: var(--text-muted); cursor: pointer;
  font-family: ui-monospace, monospace;
}
.period-btn:hover { background: var(--surface-hover); color: var(--text); }
.period-btn.on { background: var(--accent-soft-bg); color: var(--accent-soft-fg); }
.body { display: flex; flex-direction: column; }
.time-axis {
  display: flex; justify-content: space-between;
  margin-top: 4px; padding-top: 6px;
  border-top: 1px solid var(--border);
  font-size: 10px; color: var(--text-muted); font-family: ui-monospace, monospace;
}
.config-backdrop { position: absolute; inset: 0; z-index: 20; background: transparent; }
.config-pop {
  position: absolute; top: 44px; right: 12px; width: 220px;
  background: var(--surface); border: 1px solid var(--border-strong);
  border-radius: 10px; padding: 12px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.3);
  max-height: 320px; overflow-y: auto;
}
.cfg-row { display: flex; align-items: center; gap: 8px; padding: 4px 0; font-size: 12px; cursor: pointer; color: var(--text); }
.cbox {
  width: 15px; height: 15px; border-radius: 4px; flex-shrink: 0;
  border: 1px solid var(--border-strong);
  display: flex; align-items: center; justify-content: center;
  font-size: 10px; color: var(--accent-fg);
}
.cbox.on { background: var(--accent); border-color: var(--accent); }
</style>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { IconChartLine, IconAdjustments } from '@tabler/icons-vue'
import MiniChart from './MiniChart.vue'
import { useMetrics } from '../composables/useDashboardData.js'
import { availableMetrics, metricValue, metricLabel, metricColor, metricChartColor } from '../utils/metrics.js'

const props = defineProps({
  interfaces: { type: Array, required: true },
})

const { data: metrics } = useMetrics()

// Rolling history: shared timestamp array + per-key aligned value arrays.
// 150 points at the 2s poll interval ≈ the last 5 minutes.
const MAX = 150
const times = ref([])
const series = reactive({})

const STORAGE_KEY = 'cascade-ui2-diag-metrics'
function readSelected() {
  try {
    const v = JSON.parse(localStorage.getItem(STORAGE_KEY))
    if (Array.isArray(v)) return v
  } catch (_) { /* ignore */ }
  return ['cpu', 'mem']
}
const selectedKeys = ref(readSelected())
watch(selectedKeys, (v) => localStorage.setItem(STORAGE_KEY, JSON.stringify(v)), { deep: true })

const showConfig = ref(false)

const available = computed(() => availableMetrics(metrics.value, props.interfaces))
// Group available metrics for the config checklist: { group: [items] }.
const grouped = computed(() => {
  const g = {}
  for (const m of available.value) {
    (g[m.group] || (g[m.group] = [])).push(m)
  }
  return g
})

// Only render selected keys that are currently available.
const shownKeys = computed(() =>
  selectedKeys.value.filter(k => available.value.some(m => m.key === k))
)

function toggleKey(key) {
  const i = selectedKeys.value.indexOf(key)
  if (i >= 0) selectedKeys.value.splice(i, 1)
  else selectedKeys.value.push(key)
}

function currentValue(key) {
  const v = metricValue(metrics.value, key)
  if (v == null) return '—'
  if (key === 'cpu' || key === 'mem') return `${Math.round(v)}%`
  return Math.round(v).toString()
}

// Append a sample whenever the metrics snapshot updates.
watch(metrics, (snap) => {
  if (!snap || Object.keys(snap).length === 0) return
  const t = Date.now() / 1000
  const avail = available.value.map(m => m.key)
  const keys = new Set([...Object.keys(series), ...avail])
  const oldLen = times.value.length
  for (const key of keys) {
    const prev = series[key] || new Array(oldLen).fill(null)
    const v = avail.includes(key) ? metricValue(snap, key) : null
    series[key] = [...prev, v].slice(-MAX)
  }
  times.value = [...times.value, t].slice(-MAX)
})
</script>

<template>
  <div class="panel">
    <div class="head">
      <IconChartLine :size="16" style="color:#38bdf8;" />
      <span style="font-size:13px; font-weight:500;">Diagnostics</span>
      <span style="margin-left:auto; font-size:10.5px; color:var(--text-muted); font-family:ui-monospace,monospace;">last 5m</span>
      <button class="icon-btn bordered" style="width:26px; height:26px; margin-left:6px;" title="Select metrics" @click="showConfig = !showConfig">
        <IconAdjustments :size="15" />
      </button>
    </div>

    <div class="body">
      <div v-if="shownKeys.length === 0" style="padding:24px 0; text-align:center; font-size:12px; color:var(--text-muted);">
        Click the adjust icon to add metrics.
      </div>

      <template v-else>
        <div v-for="key in shownKeys" :key="key" style="margin-bottom:10px;">
          <div style="display:flex; justify-content:space-between; font-size:11px; margin-bottom:3px;">
            <span style="color:var(--text-muted);">{{ metricLabel(key, interfaces) }}</span>
            <span style="font-family:ui-monospace,monospace;" :style="{ color: metricColor(key) }">{{ currentValue(key) }}</span>
          </div>
          <MiniChart :times="times" :values="series[key] || []" :color="metricChartColor(key)" />
        </div>

        <div class="time-axis">
          <span>-5m</span><span>-4m</span><span>-3m</span><span>-2m</span><span>-1m</span><span>now</span>
        </div>
      </template>
    </div>

    <!-- Config popover -->
    <div v-if="showConfig" class="config-backdrop" @click.self="showConfig = false">
      <div class="config-pop">
        <div style="font-size:12px; font-weight:500; margin-bottom:10px;">Select metrics</div>
        <div v-for="(items, group) in grouped" :key="group" style="margin-bottom:8px;">
          <div style="font-size:10px; color:var(--text-muted); text-transform:uppercase; letter-spacing:0.04em; margin-bottom:3px;">{{ group }}</div>
          <label v-for="m in items" :key="m.key" class="cfg-row">
            <span class="cbox" :class="{ on: selectedKeys.includes(m.key) }">
              <span v-if="selectedKeys.includes(m.key)">✓</span>
            </span>
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
.body { display: flex; flex-direction: column; }
.time-axis {
  display: flex; justify-content: space-between;
  margin-top: 4px; padding-top: 6px;
  border-top: 1px solid var(--border);
  font-size: 10px; color: var(--text-muted); font-family: ui-monospace, monospace;
}
.config-backdrop {
  position: absolute; inset: 0; z-index: 20;
  background: transparent;
}
.config-pop {
  position: absolute; top: 44px; right: 12px; width: 220px;
  background: var(--surface); border: 1px solid var(--border-strong);
  border-radius: 10px; padding: 12px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.3);
  max-height: 300px; overflow-y: auto;
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

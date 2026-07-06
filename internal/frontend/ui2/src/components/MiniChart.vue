<script setup>
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'

// A compact live area chart. Shares a time window with siblings; the x-axis
// is hidden here (a single shared time axis is rendered by DiagnosticsPanel).
const props = defineProps({
  times: { type: Array, required: true },   // unix seconds
  values: { type: Array, required: true },  // numbers (may contain nulls)
  color: { type: String, required: true },  // hex stroke
  unit: { type: String, default: '' },      // tooltip suffix, e.g. '%' or ' Mbps'
  height: { type: Number, default: 42 },
  values2: { type: Array, default: null },  // optional 2nd series (e.g. tx alongside rx)
  color2: { type: String, default: null },
})

const el = ref(null)
let chart = null
let ro = null

function hexToRgba(hex, a) {
  const n = parseInt(hex.slice(1), 16)
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${a})`
}

// Cursor tooltip: a small box showing the hovered value, positioned at the
// cursor. uPlot has no built-in tooltip, so we drive one from the cursor hook.
function tooltipPlugin() {
  let tip
  return {
    hooks: {
      init: (u) => {
        tip = document.createElement('div')
        tip.className = 'mc-tip'
        u.over.appendChild(tip)
      },
      setCursor: (u) => {
        const idx = u.cursor.idx
        if (idx == null) { tip.style.display = 'none'; return }
        const v = u.data[1][idx]
        const v2 = props.values2 ? u.data[2][idx] : null
        if (v == null && v2 == null) { tip.style.display = 'none'; return }
        const t = u.data[0][idx]
        const time = t ? new Date(t * 1000).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : ''
        const fmt = (n) => Math.round(n * 10) / 10
        const parts = []
        if (v != null) parts.push(props.values2 ? `↓${fmt(v)}` : `${fmt(v)}${props.unit}`)
        if (v2 != null) parts.push(`↑${fmt(v2)}${props.unit}`)
        tip.textContent = time ? `${time}  ${parts.join(' ')}` : parts.join(' ')
        tip.style.display = 'block'
        const left = u.cursor.left
        tip.style.left = left + 'px'
        tip.style.top = '2px'
        // Flip to the left near the right edge so it stays visible.
        tip.style.transform = left > u.over.clientWidth - 60 ? 'translateX(-100%)' : 'translateX(-50%)'
      },
    },
  }
}

function build(width) {
  if (chart) { chart.destroy(); chart = null }
  const opts = {
    width: Math.max(width, 80),
    height: props.height,
    padding: [4, 0, 0, 0],
    cursor: { y: false, points: { size: 5 } },
    legend: { show: false },
    scales: { x: { time: false } },
    axes: [{ show: false }, { show: false }],
    plugins: [tooltipPlugin()],
    series: [
      {},
      {
        stroke: props.color,
        width: 1.5,
        fill: props.values2 ? undefined : hexToRgba(props.color, 0.16),
        points: { show: false },
      },
      ...(props.values2 ? [{
        stroke: props.color2,
        width: 1.5,
        points: { show: false },
      }] : []),
    ],
  }
  const data = props.values2 ? [props.times, props.values, props.values2] : [props.times, props.values]
  chart = new uPlot(opts, data, el.value)
}

onMounted(() => {
  build(el.value.clientWidth)
  ro = new ResizeObserver(entries => {
    const w = entries[0].contentRect.width
    if (chart) chart.setSize({ width: Math.max(w, 80), height: props.height })
  })
  ro.observe(el.value)
})

onBeforeUnmount(() => {
  if (ro) ro.disconnect()
  if (chart) chart.destroy()
})

watch(() => [props.times, props.values, props.values2], () => {
  if (!chart) return
  chart.setData(props.values2 ? [props.times, props.values, props.values2] : [props.times, props.values])
}, { deep: true })
</script>

<template>
  <div ref="el" class="mini-chart" />
</template>

<style scoped>
.mini-chart { width: 100%; position: relative; }
.mini-chart :deep(.u-cursor-x),
.mini-chart :deep(.u-cursor-y) { border-color: var(--border-strong); }
.mini-chart :deep(.mc-tip) {
  position: absolute;
  pointer-events: none;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 5px;
  padding: 1px 6px;
  font-size: 10.5px;
  font-family: ui-monospace, monospace;
  color: var(--text);
  white-space: nowrap;
  z-index: 5;
  display: none;
}
</style>

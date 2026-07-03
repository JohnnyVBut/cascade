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
  height: { type: Number, default: 42 },
})

const el = ref(null)
let chart = null
let ro = null

function hexToRgba(hex, a) {
  const n = parseInt(hex.slice(1), 16)
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${a})`
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
    axes: [
      { show: false },
      { show: false },
    ],
    series: [
      {},
      {
        stroke: props.color,
        width: 1.5,
        fill: hexToRgba(props.color, 0.16),
        points: { show: false },
      },
    ],
  }
  chart = new uPlot(opts, [props.times, props.values], el.value)
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

watch(() => [props.times, props.values], () => {
  if (chart) chart.setData([props.times, props.values])
}, { deep: true })
</script>

<template>
  <div ref="el" class="mini-chart" />
</template>

<style scoped>
.mini-chart { width: 100%; }
/* Neutralize uPlot's default cursor line color to fit the dark/light theme. */
.mini-chart :deep(.u-cursor-x),
.mini-chart :deep(.u-cursor-y) { border-color: var(--border-strong); }
</style>

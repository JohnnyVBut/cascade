<script setup>
import { computed, ref } from 'vue'
import { GATEWAY_STACK_COLORS as C } from '../utils/metrics.js'

// Stacked status distribution over time. bars: array of buckets, each
// { healthy, degraded, down, adminDown, time } as percentages summing to
// ~100, time in unix seconds. Rendered as vertical 100%-stacked columns
// (bottom→top: admin, down, degraded, healthy), matching the legacy gateway
// widget. Hover shows a custom instant tooltip (native <title> has a ~1s
// browser delay, which reads as sluggish).
const props = defineProps({
  bars: { type: Array, required: true },
  height: { type: Number, default: 48 },
})

const VW = 320 // viewBox width; scales to container via preserveAspectRatio=none
const cols = computed(() => {
  const n = props.bars.length
  if (n === 0) return []
  const w = VW / n
  return props.bars.map((b, i) => {
    const total = (b.healthy + b.degraded + b.down + b.adminDown) || 1
    const seg = { h: b.healthy / total, d: b.degraded / total, dn: b.down / total, ad: b.adminDown / total }
    const H = props.height
    let y = H
    const rects = []
    const push = (frac, color) => {
      const h = frac * H
      if (h <= 0) return
      y -= h
      rects.push({ y, h, color })
    }
    push(seg.ad, C.adminDown)
    push(seg.dn, C.down)
    push(seg.d, C.degraded)
    push(seg.h, C.healthy)

    const pct = { h: Math.round(seg.h * 100), d: Math.round(seg.d * 100), dn: Math.round(seg.dn * 100), ad: Math.round(seg.ad * 100) }
    let status
    if (pct.h === 100) status = 'healthy'
    else if (pct.d === 100) status = 'degraded'
    else if (pct.dn === 100) status = 'down'
    else if (pct.ad === 100) status = 'admin down'
    else {
      status = [
        pct.h ? `healthy ${pct.h}%` : '',
        pct.d ? `degraded ${pct.d}%` : '',
        pct.dn ? `down ${pct.dn}%` : '',
        pct.ad ? `admin ${pct.ad}%` : '',
      ].filter(Boolean).join(' · ')
    }
    const time = b.time
      ? new Date(b.time * 1000).toLocaleString('en-GB', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })
      : ''
    return { x: i * w, w: Math.max(w - 0.5, 0.5), rects, label: time ? `${time}  ${status}` : status }
  })
})

const tip = ref({ show: false, x: 0, label: '' })
function onMove(evt, col) {
  const rect = evt.currentTarget.closest('svg').getBoundingClientRect()
  const left = evt.clientX - rect.left
  tip.value = { show: true, x: left, label: col.label }
}
function onLeave() { tip.value.show = false }
</script>

<template>
  <div class="gw-chart">
    <svg :viewBox="`0 0 ${VW} ${height}`" preserveAspectRatio="none"
         :style="{ width: '100%', height: height + 'px', display: 'block' }">
      <g v-for="(col, i) in cols" :key="i">
        <rect v-for="(r, j) in col.rects" :key="j"
              :x="col.x" :y="r.y" :width="col.w" :height="r.h" :fill="r.color" shape-rendering="crispEdges" />
        <rect :x="col.x" y="0" :width="col.w" :height="height" fill="transparent"
              @mousemove="onMove($event, col)" @mouseleave="onLeave" />
      </g>
    </svg>
    <div v-if="tip.show" class="gw-tip"
         :style="{ left: tip.x + 'px', transform: tip.x > 220 ? 'translateX(-100%)' : 'translateX(-50%)' }">
      {{ tip.label }}
    </div>
  </div>
</template>

<style scoped>
.gw-chart { position: relative; }
.gw-tip {
  position: absolute;
  top: 2px;
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
}
</style>

<script setup>
import { computed } from 'vue'
import { GATEWAY_STACK_COLORS as C } from '../utils/metrics.js'

// Stacked status distribution over time. bars: array of buckets, each
// { healthy, degraded, down, adminDown } as percentages summing to ~100.
// Rendered as vertical 100%-stacked columns (bottom→top: admin, down,
// degraded, healthy), matching the legacy gateway widget.
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
    // Stack from bottom: adminDown, down, degraded, healthy.
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
    return { x: i * w, w: Math.max(w - 0.5, 0.5), rects }
  })
})
</script>

<template>
  <svg :viewBox="`0 0 ${VW} ${height}`" preserveAspectRatio="none"
       :style="{ width: '100%', height: height + 'px', display: 'block' }">
    <g v-for="(col, i) in cols" :key="i">
      <rect v-for="(r, j) in col.rects" :key="j"
            :x="col.x" :y="r.y" :width="col.w" :height="r.h" :fill="r.color" shape-rendering="crispEdges" />
    </g>
  </svg>
</template>

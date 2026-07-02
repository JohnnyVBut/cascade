<script setup>
import { useToast } from '../composables/useToast.js'
const { toasts } = useToast()
const toneColor = {
  success: 'var(--success)', danger: 'var(--danger)', warning: 'var(--warning)',
}
</script>

<template>
  <div style="position:fixed; bottom:20px; right:20px; display:flex; flex-direction:column; gap:8px; z-index:100;">
    <div
      v-for="t in toasts" :key="t.id"
      style="background:var(--surface); border:1px solid var(--border-strong); border-radius:var(--radius); padding:10px 14px; font-size:13px; color:var(--text); box-shadow:0 4px 16px rgba(0,0,0,0.25); display:flex; align-items:center; gap:8px; min-width:200px;"
    >
      <span :style="{ width:'6px', height:'6px', borderRadius:'50%', background: toneColor[t.tone] || 'var(--text-muted)' }" />
      {{ t.message }}
    </div>
  </div>
</template>

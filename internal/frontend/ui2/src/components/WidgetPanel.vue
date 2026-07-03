<script setup>
// Titled dashboard widget panel: colored icon + title, optional right-aligned
// summary slot, and a body slot.
defineProps({
  title: { type: String, required: true },
  iconColor: { type: String, default: 'var(--text-secondary)' },
})
</script>

<template>
  <div class="widget">
    <div class="widget-head">
      <span class="widget-icon" :style="{ color: iconColor }"><slot name="icon" /></span>
      <span class="widget-title">{{ title }}</span>
      <span class="widget-summary"><slot name="summary" /></span>
    </div>
    <slot />
  </div>
</template>

<style scoped>
.widget {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 16px;
  height: 100%;
  box-sizing: border-box;
}
.widget-head { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
.widget-icon { display: inline-flex; }
.widget-title { font-size: 13px; font-weight: 500; color: var(--text); }
.widget-summary { margin-left: auto; font-size: 12px; color: var(--text-muted); }
</style>

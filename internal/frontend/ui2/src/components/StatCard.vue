<script setup>
// Metric card: muted label above, large value with an optional muted suffix.
defineProps({
  label: { type: String, required: true },
  value: { type: [String, Number], required: true },
  suffix: { type: String, default: '' },
})
</script>

<template>
  <div class="stat">
    <div class="stat-label">{{ label }}</div>
    <div class="stat-value">
      {{ value }}<span v-if="suffix" class="stat-suffix"> {{ suffix }}</span>
    </div>
  </div>
</template>

<style scoped>
.stat {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 14px 16px;
}
.stat-label { font-size: 12px; color: var(--text-muted); margin-bottom: 6px; }
.stat-value { font-size: 24px; font-weight: 500; color: var(--text); }
.stat-suffix { font-size: 13px; font-weight: 400; color: var(--text-muted); }
</style>

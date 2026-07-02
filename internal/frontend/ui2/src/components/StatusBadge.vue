<script setup>
// Status pill. tone: success | warning | danger | idle.
defineProps({
  tone: { type: String, default: 'idle' },
  label: { type: String, required: true },
})
</script>

<template>
  <span class="badge" :class="'badge-' + tone">{{ label }}</span>
</template>

<style scoped>
.badge {
  font-size: 11px;
  padding: 3px 9px;
  border-radius: 20px;
  white-space: nowrap;
}
.badge-success { background: var(--success-bg); color: var(--success-fg); }
.badge-warning { background: var(--warning-bg); color: var(--warning-fg); }
.badge-danger  { background: var(--danger-bg);  color: var(--danger-fg); }
.badge-idle    { background: var(--idle-bg);    color: var(--idle-fg); }
</style>

<script setup>
// Raised surface card. Padding can be disabled for custom layouts (e.g. rows).
defineProps({
  pad: { type: Boolean, default: true },
})
</script>

<template>
  <div class="card" :style="pad ? 'padding:14px 16px;' : ''">
    <slot />
  </div>
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
}
</style>

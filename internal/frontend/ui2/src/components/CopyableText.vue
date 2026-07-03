<script setup>
import { ref } from 'vue'
import { IconCopy, IconCheck } from '@tabler/icons-vue'
import { useToast } from '../composables/useToast.js'

// Click-to-copy wrapper for IP addresses / endpoints. Shows a brief inline
// checkmark instead of a toast so rapid clicking (e.g. scanning a peer list)
// doesn't spam notifications.
const props = defineProps({
  value: { type: String, required: true }, // the text to display AND copy
  copyValue: { type: String, default: '' }, // optional: copy this instead of `value` (e.g. value has extra formatting)
})
const { push } = useToast()
const copied = ref(false)

async function copy() {
  const text = props.copyValue || props.value
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1200)
  } catch (_) {
    push('Copy failed', 'danger')
  }
}
</script>

<template>
  <span class="copyable" @click.stop="copy" :title="'Click to copy: ' + (copyValue || value)">
    {{ value }}
    <IconCheck v-if="copied" :size="11" class="copy-icon copy-icon-ok" />
    <IconCopy v-else :size="11" class="copy-icon" />
  </span>
</template>

<style scoped>
.copyable {
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.copyable:hover { color: var(--text); }
.copy-icon { opacity: 0; transition: opacity 0.1s; flex-shrink: 0; }
.copyable:hover .copy-icon { opacity: 0.6; }
.copy-icon-ok { opacity: 1 !important; color: var(--success-fg); }
</style>

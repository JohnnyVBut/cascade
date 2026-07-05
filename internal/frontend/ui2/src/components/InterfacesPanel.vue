<script setup>
import { IconNetwork } from '@tabler/icons-vue'
import InterfaceCompactRow from './InterfaceCompactRow.vue'

const props = defineProps({
  interfaces: { type: Array, required: true },
})
const emit = defineEmits(['changed', 'add-peer'])
</script>

<template>
  <div class="panel">
    <div class="head">
      <IconNetwork :size="16" style="color:var(--success);" />
      <span style="font-size:13px; font-weight:500;">Interfaces</span>
      <span style="margin-left:auto; font-size:12px; color:var(--text-muted);">
        {{ interfaces.filter(i => i.enabled).length }} / {{ interfaces.length }} up
      </span>
    </div>

    <div class="list">
      <div v-if="interfaces.length === 0" style="padding:20px 0; text-align:center; font-size:12px; color:var(--text-muted);">
        No interfaces yet.
      </div>
      <InterfaceCompactRow
        v-for="iface in interfaces" :key="iface.id"
        :iface="iface"
        @changed="emit('changed')"
        @add-peer="emit('add-peer', $event)"
      />
    </div>
  </div>
</template>

<style scoped>
.panel {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  display: flex; flex-direction: column;
  height: 100%;
}
.head { display: flex; align-items: center; gap: 8px; padding: 12px 14px; border-bottom: 1px solid var(--border); flex-shrink: 0; }
.list { flex: 1; overflow-y: auto; padding: 0 14px; max-height: 560px; }
</style>

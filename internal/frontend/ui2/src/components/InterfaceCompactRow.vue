<script setup>
import { IconPlus, IconRefresh, IconSettings } from '@tabler/icons-vue'
import BaseToggle from './BaseToggle.vue'
import { startInterface, stopInterface, restartInterface } from '../composables/useDashboardData.js'
import { useToast } from '../composables/useToast.js'
import { fmtRate } from '../utils/format.js'
import { ref, computed } from 'vue'

const props = defineProps({
  iface: { type: Object, required: true },
  rate: { type: Object, default: null },
})
const emit = defineEmits(['changed', 'add-peer'])
const { push } = useToast()
const busy = ref(false)

const peerCount = computed(() => (props.iface.peers && props.iface.peers.length) || 0)
const rateLabel = computed(() => {
  if (!props.rate) return ''
  const rx = fmtRate(props.rate.rxMbps || 0)
  const tx = fmtRate(props.rate.txMbps || 0)
  return ` · ↓${rx.value} ↑${tx.value} Mbps`
})

async function toggle() {
  if (busy.value) return
  busy.value = true
  try {
    if (props.iface.enabled) await stopInterface(props.iface.id)
    else await startInterface(props.iface.id)
    emit('changed')
  } catch (e) {
    push(e.message || 'Failed', 'danger')
  } finally {
    busy.value = false
  }
}

async function restart() {
  try {
    await restartInterface(props.iface.id)
    push(`${props.iface.name} restarted`)
    emit('changed')
  } catch (e) {
    push(e.message || 'Restart failed', 'danger')
  }
}

function soon() { push('Coming soon', 'warning') }
</script>

<template>
  <div class="row">
    <span class="dot" :style="{ background: iface.enabled ? 'var(--success)' : 'var(--idle)' }" />
    <div style="min-width:0; flex:1;">
      <div style="font-size:13px; font-weight:500; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">
        {{ iface.name }}
        <span style="font-size:11px; font-weight:400; color:var(--text-muted); font-family:ui-monospace,monospace;">{{ iface.address }}</span>
      </div>
      <div style="font-size:11px; color:var(--text-secondary); overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">
        {{ iface.protocol === 'amneziawg-2.0' ? 'AmneziaWG' : 'WireGuard' }} · :{{ iface.listenPort }} · {{ peerCount }} peers{{ rateLabel }}
      </div>
    </div>
    <div class="actions">
      <button class="icon-btn" title="Add peer" @click="emit('add-peer', iface)"><IconPlus :size="15" /></button>
      <button class="icon-btn" title="Restart" @click="restart"><IconRefresh :size="15" /></button>
      <button class="icon-btn" title="Edit" @click="soon"><IconSettings :size="15" /></button>
      <BaseToggle :model-value="iface.enabled" :disabled="busy" @update:model-value="toggle" />
    </div>
  </div>
</template>

<style scoped>
.row {
  display: flex; align-items: center; gap: 10px;
  padding: 9px 4px;
  border-bottom: 1px solid var(--border);
}
.dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
.actions { display: flex; align-items: center; gap: 2px; flex-shrink: 0; }
</style>

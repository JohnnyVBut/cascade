<script setup>
import { ref, computed } from 'vue'
import { IconPlus, IconRefresh, IconSettings } from '@tabler/icons-vue'
import BaseToggle from './BaseToggle.vue'
import PeerRow from './PeerRow.vue'
import { startInterface, stopInterface, restartInterface } from '../composables/useDashboardData.js'
import { useToast } from '../composables/useToast.js'
import { fmtRate } from '../utils/format.js'

const props = defineProps({
  iface: { type: Object, required: true },
  rate: { type: Object, default: null }, // { rxMbps, txMbps } or null
})
const emit = defineEmits(['changed'])
const { push } = useToast()

const expanded = ref(false)
const busy = ref(false)
const COLLAPSED = 5

const peers = computed(() => props.iface.peers || [])
const shown = computed(() => expanded.value ? peers.value : peers.value.slice(0, COLLAPSED))

const rateLabel = computed(() => {
  if (!props.rate) return ''
  const rx = fmtRate(props.rate.rxMbps || 0)
  const tx = fmtRate(props.rate.txMbps || 0)
  return ` · ↓${rx.value}${rx.unit === 'Gbps' ? 'G' : ''} ↑${tx.value}${tx.unit === 'Gbps' ? 'G' : ''} Mbps`
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

// Create/edit forms land in a follow-up commit.
function soon() { push('Coming soon', 'warning') }
</script>

<template>
  <div class="iface">
    <div class="iface-head">
      <span class="dot" :style="{ background: iface.enabled ? 'var(--success)' : 'var(--idle)' }" />
      <div style="min-width:0;">
        <div style="font-size:15px; font-weight:500;">
          {{ iface.name }}
          <span style="font-size:12px; font-weight:400; color:var(--text-muted); font-family:ui-monospace,monospace;">{{ iface.address }}</span>
        </div>
        <div style="font-size:12px; color:var(--text-secondary); margin-top:1px;">
          {{ iface.protocol === 'amneziawg-2.0' ? 'AmneziaWG' : 'WireGuard' }}
          · :{{ iface.listenPort }} · {{ peers.length }} peers{{ rateLabel }}
        </div>
      </div>
      <div class="iface-actions">
        <button class="icon-btn bordered" title="Add peer" @click="soon"><IconPlus :size="17" /></button>
        <button class="icon-btn bordered" title="Restart" @click="restart"><IconRefresh :size="17" /></button>
        <button class="icon-btn bordered" title="Edit" @click="soon"><IconSettings :size="17" /></button>
        <BaseToggle :model-value="iface.enabled" :disabled="busy" @update:model-value="toggle" style="margin-left:4px;" />
      </div>
    </div>

    <div v-if="peers.length === 0" style="padding:14px 16px; font-size:12px; color:var(--text-muted);">
      No peers yet.
    </div>
    <PeerRow
      v-for="p in shown" :key="p.id"
      :peer="p" :iface-id="iface.id"
      @changed="emit('changed')"
    />
    <div
      v-if="peers.length > COLLAPSED"
      class="show-all"
      @click="expanded = !expanded"
    >
      {{ expanded ? 'Show less' : `Show all ${peers.length} peers →` }}
    </div>
  </div>
</template>

<style scoped>
.iface {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  overflow: hidden;
}
.iface-head {
  display: flex; align-items: center; gap: 12px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--border);
}
.dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.iface-actions { margin-left: auto; display: flex; align-items: center; gap: 4px; }
.show-all {
  padding: 9px 16px;
  border-top: 1px solid var(--border);
  font-size: 12px; color: var(--accent-soft-fg); cursor: pointer;
}
.show-all:hover { background: var(--surface-hover); }
</style>

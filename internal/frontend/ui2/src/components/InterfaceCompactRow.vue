<script setup>
import { IconPlus, IconRefresh, IconSettings, IconPlug } from '@tabler/icons-vue'
import BaseToggle from './BaseToggle.vue'
import CopyableText from './CopyableText.vue'
import { startInterface, stopInterface, restartInterface } from '../composables/useDashboardData.js'
import { useToast } from '../composables/useToast.js'
import { fmtBytes } from '../utils/format.js'
import { ref, computed } from 'vue'

const props = defineProps({
  iface: { type: Object, required: true },
})
const emit = defineEmits(['changed', 'add-peer'])
const { push } = useToast()
const busy = ref(false)

const peerCount = computed(() => (props.iface.peers && props.iface.peers.length) || 0)
const isAwg = computed(() => props.iface.protocol === 'amneziawg-2.0')
const protocolLabel = computed(() => (isAwg.value ? 'awg2.0' : 'wg1.0'))
const roleLabel = computed(() => (props.iface.disableRoutes ? 'S2S' : 'server'))
const displayName = computed(() => {
  const name = props.iface.name || ''
  return name.length > 25 ? name.slice(0, 25) + '…' : name
})

// Aggregate lifetime totals across this interface's peers (colored like the
// legacy dashboard: download red, upload green).
const totals = computed(() => {
  const peers = props.iface.peers || []
  return {
    rx: peers.reduce((s, p) => s + (p.totalRx || 0), 0),
    tx: peers.reduce((s, p) => s + (p.totalTx || 0), 0),
  }
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
    <div style="min-width:0; flex:1;">
      <div class="grid-row">
        <span class="dot" :style="{ background: iface.enabled ? 'var(--success)' : 'var(--idle)' }" />
        <span class="cell cell-name name-tip-wrap">
          {{ displayName }}
          <span v-if="iface.name !== displayName" class="name-tip">{{ iface.name }}</span>
        </span>
        <span class="cell cell-sysname">
          <span class="tag" :class="isAwg ? 'tag-awg' : 'tag-wg'">{{ protocolLabel }}</span>
        </span>
        <span class="cell cell-ip"><CopyableText :value="iface.address" /></span>
        <span class="cell cell-port"><IconPlug :size="12" style="display:inline-block; vertical-align:-1px; margin-right:2px; flex-shrink:0;" />{{ iface.listenPort }}</span>
        <span class="cell cell-traffic">
          <span style="color:var(--success-fg);">↑{{ fmtBytes(totals.tx) }}</span>
        </span>
      </div>
      <div class="grid-row" style="color:var(--text-secondary);">
        <span class="dot-spacer"></span>
        <span class="cell cell-protocol-spacer"></span>
        <span class="cell cell-protocol" :title="iface.id">{{ iface.id }}</span>
        <span class="cell cell-role-spacer"></span>
        <span class="cell cell-role">
          <span class="tag" :class="iface.disableRoutes ? 'tag-s2s' : 'tag-server'">{{ roleLabel }}</span>
        </span>
        <span class="cell cell-peers-spacer"></span>
        <span class="cell cell-peers">{{ peerCount }} peers</span>
        <span class="cell cell-traffic-spacer"></span>
        <span class="cell cell-traffic">
          <span style="color:var(--danger-fg);">↓{{ fmtBytes(totals.rx) }}</span>
        </span>
      </div>
    </div>
    <div class="actions">
      <button class="icon-btn" title="Add peer" @click="emit('add-peer', iface)"><IconPlus :size="19" /></button>
      <button class="icon-btn" title="Restart" @click="restart"><IconRefresh :size="19" /></button>
      <button class="icon-btn" title="Edit" @click="soon"><IconSettings :size="19" /></button>
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
.dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; margin-right: 8px; }
.dot-spacer { width: 15px; flex-shrink: 0; }
.grid-row {
  display: flex; align-items: center;
  font-size: 12px; font-family: ui-monospace, monospace;
  line-height: 1.6;
}
.cell { text-align: left; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex-shrink: 0; }
.cell-name { width: 25ch; font-family: system-ui, sans-serif; font-size: 13px; font-weight: 500; }
.name-tip-wrap { position: relative; }
.name-tip-wrap:hover { overflow: visible; }
.name-tip {
  display: none;
  position: absolute; left: 0; top: 100%; margin-top: 4px; z-index: 10;
  background: var(--surface); border: 1px solid var(--border);
  color: var(--text-primary); font-weight: 400; font-size: 12px;
  padding: 4px 8px; border-radius: 4px; white-space: nowrap;
}
.name-tip-wrap:hover .name-tip { display: block; }
.cell-sysname { width: 8ch; color: var(--text-muted); }
.cell-ip { width: 18ch; color: var(--text-muted); }
.cell-port { width: 8ch; display: inline-flex; align-items: center; }
.cell-protocol-spacer { width: 210.63px; }
.cell-protocol { width: 10ch; }
.cell-role-spacer { width: 0px; }
.cell-role { width: 18ch; color: var(--text-muted); margin-left: -14.44px; }
.cell-peers-spacer { width: 0px; }
.cell-traffic-spacer { width: 0px; }
.cell-traffic { width: 18ch; padding-right: 12px; margin-left: 24px; }
.cell-peers { width: 8ch; }
.tag {
  font-size: 9.5px; font-weight: 600; padding: 1px 5px; border-radius: 3px;
  white-space: nowrap;
}
.tag-wg { background: var(--idle-bg); color: var(--text-secondary); }
.tag-awg { background: var(--accent-soft-bg); color: var(--accent-soft-fg); }
.tag-server { background: var(--idle-bg); color: var(--text-secondary); }
.tag-s2s { background: var(--accent-soft-bg); color: var(--accent-soft-fg); }
.actions { display: flex; align-items: center; gap: 4px; flex-shrink: 0; }
.actions .icon-btn { width: 30px; height: 30px; }
</style>

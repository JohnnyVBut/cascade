<script setup>
import { ref, computed } from 'vue'
import { IconQrcode, IconDownload, IconTrash } from '@tabler/icons-vue'
import BaseToggle from './BaseToggle.vue'
import QrModal from './QrModal.vue'
import CopyableText from './CopyableText.vue'
import { apiUrl } from '../api/client.js'
import { enablePeer, disablePeer, deletePeer } from '../composables/useDashboardData.js'
import { useToast } from '../composables/useToast.js'
import { fmtBytes, fmtAgo, fmtKbps, effectiveRate, expiryUrgency, fmtDateShort, peerPublicIP } from '../utils/format.js'

const props = defineProps({
  peer: { type: Object, required: true }, // includes interfaceId, interfaceName, peerType
  groups: { type: Array, default: () => [] },
})
const emit = defineEmits(['changed'])
const { push } = useToast()

const showQr = ref(false)
const busy = ref(false)

const configUrl = () => apiUrl(`/tunnel-interfaces/${props.peer.interfaceId}/peers/${props.peer.id}/config`)
const qrUrl = () => apiUrl(`/tunnel-interfaces/${props.peer.interfaceId}/peers/${props.peer.id}/qrcode.svg`)

const recentlySeen = () => {
  if (!props.peer.latestHandshakeAt) return false
  return Date.now() - new Date(props.peer.latestHandshakeAt).getTime() < 600000
}

const groupName = computed(() => {
  if (!props.peer.groupId) return ''
  const g = props.groups.find(g => g.id === props.peer.groupId)
  return g ? g.name : ''
})

const rate = computed(() => effectiveRate(props.peer, props.groups))
const rateLabel = computed(() => {
  const r = rate.value
  const parts = []
  if (r.rateDown > 0) parts.push('↓' + fmtKbps(r.rateDown))
  if (r.rateUp > 0) parts.push('↑' + fmtKbps(r.rateUp))
  return parts.join(' ')
})

const urgency = computed(() => expiryUrgency(props.peer.expiredAt))

// S2S peers show the full endpoint (host is meaningful, e.g. a static peer
// address); client peers show just the public IP (port is ephemeral/NAT).
const publicAddr = computed(() => {
  if (!props.peer.runtimeEndpoint) return ''
  return props.peer.peerType === 'interconnect'
    ? props.peer.runtimeEndpoint
    : peerPublicIP(props.peer.runtimeEndpoint)
})

async function toggle() {
  if (busy.value) return
  busy.value = true
  try {
    if (props.peer.enabled) await disablePeer(props.peer.interfaceId, props.peer.id)
    else await enablePeer(props.peer.interfaceId, props.peer.id)
    emit('changed')
  } catch (e) {
    push(e.message || 'Failed', 'danger')
  } finally {
    busy.value = false
  }
}

async function remove() {
  if (!confirm(`Delete peer "${props.peer.name}"?`)) return
  try {
    await deletePeer(props.peer.interfaceId, props.peer.id)
    push('Peer deleted')
    emit('changed')
  } catch (e) {
    push(e.message || 'Delete failed', 'danger')
  }
}
</script>

<template>
  <div class="peer-dense-row" :style="{ opacity: peer.enabled ? 1 : 0.55 }">
    <span class="dot" :style="{ background: recentlySeen() ? 'var(--success)' : 'var(--idle)' }" />
    <div style="min-width:0; flex:1;">
      <div style="font-size:12px; font-weight:500; white-space:nowrap; overflow:hidden; text-overflow:ellipsis;">
        {{ peer.name }}
        <span class="tag">{{ peer.interfaceName }}</span>
        <span v-if="peer.peerType === 'interconnect'" class="tag tag-s2s">S2S</span>
        <span v-if="groupName" class="tag tag-group" :title="'Group: ' + groupName">{{ groupName }}</span>
        <span v-if="rateLabel" class="tag" :class="rate.fromGroup ? 'tag-rate-group' : 'tag-rate-own'" :title="(rate.fromGroup ? 'Group limit: ' : 'Rate limit: ') + rateLabel">
          <span v-if="rate.fromGroup" style="opacity:0.75;">G</span>{{ rateLabel }}
        </span>
        <span v-if="urgency" class="tag" :class="'tag-exp-' + urgency" :title="'Expires: ' + fmtDateShort(peer.expiredAt)">
          {{ fmtDateShort(peer.expiredAt) }}
        </span>
      </div>
      <div style="font-size:10.5px; color:var(--text-muted); font-family:ui-monospace,monospace; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">
        <CopyableText :value="(peer.address || '').split('/')[0]" />
        <template v-if="publicAddr">· <CopyableText :value="publicAddr" /></template>
        · {{ fmtAgo(peer.latestHandshakeAt) }}
      </div>
    </div>
    <span v-if="peer.enabled" style="font-size:10.5px; font-family:ui-monospace,monospace; flex-shrink:0; text-align:right;">
      <span style="color:var(--danger-fg);">↓{{ fmtBytes(peer.totalRx) }}</span>
      <span style="color:var(--success-fg); margin-left:4px;">↑{{ fmtBytes(peer.totalTx) }}</span>
    </span>
    <div class="actions">
      <button class="icon-btn sm" :disabled="!peer.downloadableConfig" title="QR code" @click="showQr = true"><IconQrcode :size="14" /></button>
      <a v-if="peer.downloadableConfig" class="icon-btn sm" :href="configUrl()" :download="peer.name + '.conf'" title="Download config"><IconDownload :size="14" /></a>
      <button v-else class="icon-btn sm" disabled title="No private key"><IconDownload :size="14" /></button>
      <button class="icon-btn sm" title="Delete" @click="remove"><IconTrash :size="14" /></button>
      <BaseToggle :model-value="peer.enabled" :disabled="busy" @update:model-value="toggle" />
    </div>

    <QrModal v-if="showQr" :url="qrUrl()" :title="peer.name" @close="showQr = false" />
  </div>
</template>

<style scoped>
.peer-dense-row {
  display: flex; align-items: center; gap: 8px;
  padding: 6px 4px;
  border-bottom: 1px solid var(--border);
}
.dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
.tag {
  font-size: 9.5px; font-weight: 600; padding: 1px 5px; border-radius: 3px; margin-left: 4px;
  background: var(--idle-bg); color: var(--text-secondary);
  white-space: nowrap;
}
.tag-s2s { background: var(--accent-soft-bg); color: var(--accent-soft-fg); }
.tag-group { background: var(--success-bg); color: var(--success-fg); }
.tag-rate-own { background: var(--warning-bg); color: var(--warning-fg); }
.tag-rate-group { background: var(--accent-soft-bg); color: var(--accent-soft-fg); }
.tag-exp-expired { background: var(--danger-bg); color: var(--danger-fg); }
.tag-exp-soon { background: var(--warning-bg); color: var(--warning-fg); }
.tag-exp-far { background: var(--idle-bg); color: var(--text-secondary); }
.actions { display: flex; align-items: center; gap: 1px; flex-shrink: 0; }
.icon-btn.sm { width: 22px; height: 22px; }
</style>

<script setup>
import { ref } from 'vue'
import { IconQrcode, IconDownload, IconTrash } from '@tabler/icons-vue'
import BaseToggle from './BaseToggle.vue'
import QrModal from './QrModal.vue'
import { apiUrl } from '../api/client.js'
import { enablePeer, disablePeer, deletePeer } from '../composables/useDashboardData.js'
import { useToast } from '../composables/useToast.js'
import { fmtBytes, fmtAgo, initials } from '../utils/format.js'

const props = defineProps({
  peer: { type: Object, required: true },
  ifaceId: { type: String, required: true },
})
const emit = defineEmits(['changed'])
const { push } = useToast()

const showQr = ref(false)
const busy = ref(false)

const configUrl = () => apiUrl(`/tunnel-interfaces/${props.ifaceId}/peers/${props.peer.id}/config`)
const qrUrl = () => apiUrl(`/tunnel-interfaces/${props.ifaceId}/peers/${props.peer.id}/qrcode.svg`)

async function toggle() {
  if (busy.value) return
  busy.value = true
  try {
    if (props.peer.enabled) await disablePeer(props.ifaceId, props.peer.id)
    else await enablePeer(props.ifaceId, props.peer.id)
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
    await deletePeer(props.ifaceId, props.peer.id)
    push('Peer deleted')
    emit('changed')
  } catch (e) {
    push(e.message || 'Delete failed', 'danger')
  }
}
</script>

<template>
  <div class="peer-row" :class="{ off: !peer.enabled }">
    <div class="avatar">{{ initials(peer.name) }}</div>
    <div style="min-width:0;">
      <div style="font-size:13px; font-weight:500; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;"
           :style="{ color: peer.enabled ? 'var(--text)' : 'var(--text-secondary)' }">
        {{ peer.name }}
      </div>
      <div style="font-size:11px; color:var(--text-muted); font-family:ui-monospace,monospace;">
        {{ (peer.address || '').split('/')[0] }} · {{ peer.enabled ? fmtAgo(peer.latestHandshakeAt) : 'disabled' }}
      </div>
    </div>
    <div class="peer-actions">
      <span v-if="peer.enabled" style="font-size:11px; color:var(--text-secondary); font-family:ui-monospace,monospace; margin-right:4px;">
        ↓{{ fmtBytes(peer.totalRx) }} ↑{{ fmtBytes(peer.totalTx) }}
      </span>
      <button class="icon-btn" :disabled="!peer.downloadableConfig" title="QR code" @click="showQr = true"><IconQrcode :size="17" /></button>
      <a v-if="peer.downloadableConfig" class="icon-btn" :href="configUrl()" :download="peer.name + '.conf'" title="Download config"><IconDownload :size="17" /></a>
      <button v-else class="icon-btn" disabled title="No private key"><IconDownload :size="17" /></button>
      <button class="icon-btn" title="Delete" @click="remove"><IconTrash :size="17" /></button>
      <BaseToggle :model-value="peer.enabled" :disabled="busy" @update:model-value="toggle" />
    </div>

    <QrModal v-if="showQr" :url="qrUrl()" :title="peer.name" @close="showQr = false" />
  </div>
</template>

<style scoped>
.peer-row {
  display: flex; align-items: center; gap: 10px;
  padding: 9px 16px;
  border-top: 1px solid var(--border);
}
.avatar {
  width: 26px; height: 26px; border-radius: 50%; flex-shrink: 0;
  background: var(--accent-soft-bg); color: var(--accent-soft-fg);
  display: flex; align-items: center; justify-content: center;
  font-size: 11px; font-weight: 500;
}
.off .avatar { background: var(--idle-bg); color: var(--idle-fg); }
.peer-actions { margin-left: auto; display: flex; align-items: center; gap: 2px; }
</style>

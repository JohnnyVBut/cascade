<script setup>
import { ref, computed } from 'vue'
import { IconUsers } from '@tabler/icons-vue'
import PeerDenseRow from './PeerDenseRow.vue'
import { useClientGroups } from '../composables/useDashboardData.js'

const props = defineProps({
  interfaces: { type: Array, required: true },
})
const emit = defineEmits(['changed'])

const { data: groups } = useClientGroups()

const ifaceFilter = ref('')
const sortBy = ref('name')
const search = ref('')

// Flatten peers across all interfaces, tagging each with interface context.
const allPeers = computed(() => {
  const out = []
  for (const iface of props.interfaces) {
    for (const p of (iface.peers || [])) {
      out.push({ ...p, interfaceId: iface.id, interfaceName: iface.name })
    }
  }
  return out
})

// Recently active = handshake within the last 3 minutes (mirrors legacy
// peers-summary "Connected" stat).
const connectedCount = computed(() =>
  allPeers.value.filter(p => p.latestHandshakeAt && (Date.now() - new Date(p.latestHandshakeAt).getTime()) < 3 * 60 * 1000).length
)

// Group name lookup used both for the tag and for search matching.
function groupNameFor(p) {
  if (!p.groupId) return ''
  const g = groups.value.find(g => g.id === p.groupId)
  return g ? g.name : ''
}

const filtered = computed(() => {
  let peers = allPeers.value
  if (ifaceFilter.value === '__clients__') {
    peers = peers.filter(p => p.peerType !== 'interconnect')
  } else if (ifaceFilter.value === '__s2s__') {
    peers = peers.filter(p => p.peerType === 'interconnect')
  } else if (ifaceFilter.value) {
    peers = peers.filter(p => p.interfaceId === ifaceFilter.value)
  }

  const q = search.value.trim().toLowerCase()
  if (q) {
    peers = peers.filter(p => [
      p.name, p.address, p.interfaceName, p.runtimeEndpoint, groupNameFor(p),
    ].filter(Boolean).some(field => field.toLowerCase().includes(q)))
  }

  const sorted = [...peers]
  if (sortBy.value === 'traffic') {
    sorted.sort((a, b) => ((b.totalTx || 0) + (b.totalRx || 0)) - ((a.totalTx || 0) + (a.totalRx || 0)))
  } else if (sortBy.value === 'seen') {
    sorted.sort((a, b) => {
      const ta = a.latestHandshakeAt ? new Date(a.latestHandshakeAt).getTime() : 0
      const tb = b.latestHandshakeAt ? new Date(b.latestHandshakeAt).getTime() : 0
      return tb - ta
    })
  } else {
    sorted.sort((a, b) => (a.name || '').localeCompare(b.name || ''))
  }
  return sorted
})
</script>

<template>
  <div class="panel">
    <div class="head">
      <IconUsers :size="16" style="color:#f472b6;" />
      <span style="font-size:13px; font-weight:500;">Peers</span>
      <span style="margin-left:auto; display:flex; align-items:center; gap:8px; font-size:12px; color:var(--text-muted);">
        <span style="color:var(--success-fg);">{{ connectedCount }} connected</span>
        <span>· {{ filtered.length }}</span>
      </span>
    </div>

    <div class="filter-bar">
      <select v-model="ifaceFilter" class="select">
        <option value="">All peers</option>
        <option value="__clients__">All clients</option>
        <option value="__s2s__">All S2S</option>
        <option v-for="iface in interfaces" :key="iface.id" :value="iface.id">{{ iface.name }}</option>
      </select>
      <select v-model="sortBy" class="select">
        <option value="name">Sort: Name</option>
        <option value="traffic">Sort: Traffic</option>
        <option value="seen">Sort: Last seen</option>
      </select>
      <input v-model="search" type="text" class="search" placeholder="Search…" />
    </div>

    <div class="list">
      <div v-if="filtered.length === 0" style="grid-column:1/-1; padding:20px 0; text-align:center; font-size:12px; color:var(--text-muted);">
        No peers
      </div>
      <PeerDenseRow
        v-for="p in filtered" :key="p.id"
        :peer="p" :groups="groups"
        @changed="emit('changed')"
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
.filter-bar { display: flex; gap: 6px; padding: 8px 14px; border-bottom: 1px solid var(--border); flex-shrink: 0; }
.select {
  flex: 1; font-size: 11.5px; padding: 4px 6px;
  border: 1px solid var(--border-strong); border-radius: 5px;
  background: var(--surface); color: var(--text); cursor: pointer;
}
.search {
  flex: 1; font-size: 11.5px; padding: 4px 6px;
  border: 1px solid var(--border-strong); border-radius: 5px;
  background: var(--surface); color: var(--text);
}
.list {
  flex: 1; overflow-y: auto; overflow-x: hidden; padding: 6px 14px; max-height: 560px;
  display: grid;
  grid-template-columns: auto 1fr auto auto;
  align-content: start;
  column-gap: 8px; row-gap: 2px;
  align-items: center;
}
</style>

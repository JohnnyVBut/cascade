<script setup>
// QR code modal. url points at the peer qrcode.svg endpoint (same-origin,
// cookie-authenticated). Emits close on backdrop click or button.
defineProps({
  url: { type: String, required: true },
  title: { type: String, default: '' },
})
const emit = defineEmits(['close'])
</script>

<template>
  <div class="backdrop" @click.self="emit('close')">
    <div class="qr-card">
      <div style="font-size:14px; font-weight:500; margin-bottom:12px;">{{ title }}</div>
      <div style="background:#fff; border-radius:8px; padding:12px; display:flex; justify-content:center;">
        <img :src="url" alt="Peer QR code" style="width:220px; height:220px; display:block;" />
      </div>
      <button class="close-btn" @click="emit('close')">Close</button>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; z-index: 90;
  background: rgba(0,0,0,0.55);
  display: flex; align-items: center; justify-content: center;
  padding: 24px;
}
.qr-card {
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 12px;
  padding: 18px;
  max-width: 280px;
}
.close-btn {
  margin-top: 14px; width: 100%;
  font-size: 13px; font-weight: 500;
  padding: 8px; border-radius: var(--radius);
  border: 1px solid var(--border-strong); background: transparent; color: var(--text);
  cursor: pointer;
}
.close-btn:hover { background: var(--surface-hover); }
</style>

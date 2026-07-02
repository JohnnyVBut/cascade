<script setup>
// Variants: primary (accent), secondary (outlined), ghost (text), danger.
defineProps({
  variant: { type: String, default: 'secondary' },
})
</script>

<template>
  <button :class="['btn', 'btn-' + variant]">
    <slot />
  </button>
</template>

<style scoped>
.btn {
  font-size: 13px;
  font-weight: 500;
  padding: 8px 16px;
  border-radius: var(--radius);
  border: 1px solid transparent;
  cursor: pointer;
  transition: background 0.12s, border-color 0.12s, opacity 0.12s;
  line-height: 1.2;
}
.btn:active { transform: scale(0.98); }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }

.btn-primary { background: var(--accent); color: var(--accent-fg); }
.btn-primary:hover:not(:disabled) { background: var(--accent-hover); }

.btn-secondary { background: transparent; color: var(--text); border-color: var(--border-strong); }
.btn-secondary:hover:not(:disabled) { background: var(--surface-hover); }

.btn-ghost { background: transparent; color: var(--text-secondary); }
.btn-ghost:hover:not(:disabled) { background: var(--surface-hover); color: var(--text); }

.btn-danger { background: transparent; color: var(--danger-fg); border-color: var(--danger-border); }
.btn-danger:hover:not(:disabled) { background: var(--danger-bg); }
</style>

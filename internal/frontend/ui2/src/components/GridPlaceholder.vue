<script setup>
// Invisible spacer for the dashboard's 4-column grid (.grid4). Use to
// explicitly pad out a row to a full 4/4 instead of relying on the grid's
// automatic wrap behavior, which packs widgets unpredictably as their count
// or order changes. span: 1 | 2 | 3 | 4 (quarters).
defineProps({
  span: { type: [Number, String], default: 1 },
})
</script>

<template>
  <div class="grid-placeholder" :class="'g-' + span" aria-hidden="true" />
</template>

<style scoped>
.grid-placeholder {
  visibility: hidden;
  pointer-events: none;
}
</style>

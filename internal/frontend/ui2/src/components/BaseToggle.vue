<script setup>
// On/off switch. v-model:modelValue (boolean). Emits when clicked unless disabled.
const props = defineProps({
  modelValue: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])

function toggle() {
  if (props.disabled) return
  emit('update:modelValue', !props.modelValue)
}
</script>

<template>
  <button
    class="toggle"
    :class="{ on: modelValue, disabled }"
    role="switch"
    :aria-checked="modelValue"
    @click="toggle"
  >
    <span class="knob" />
  </button>
</template>

<style scoped>
.toggle {
  width: 34px;
  height: 20px;
  border-radius: 20px;
  border: none;
  background: var(--border-strong);
  position: relative;
  cursor: pointer;
  padding: 0;
  transition: background 0.15s;
}
.toggle.on { background: var(--accent); }
.toggle.disabled { opacity: 0.5; cursor: not-allowed; }
.knob {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #fff;
  transition: transform 0.15s;
}
.toggle.on .knob { transform: translateX(14px); }
</style>

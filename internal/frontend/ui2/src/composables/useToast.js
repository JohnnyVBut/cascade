import { ref } from 'vue'

// Minimal toast queue. toasts are { id, message, tone }.
const toasts = ref([])
let nextId = 1

export function useToast() {
  function push(message, tone = 'success') {
    const id = nextId++
    toasts.value.push({ id, message, tone })
    setTimeout(() => dismiss(id), 3000)
  }
  function dismiss(id) {
    toasts.value = toasts.value.filter(t => t.id !== id)
  }
  return { toasts, push, dismiss }
}

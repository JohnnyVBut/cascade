import { ref, watch } from 'vue'

// Theme: 'light' | 'dark' | 'auto'. 'auto' follows the OS preference.
// The chosen mode is persisted; the resolved (light|dark) value drives the
// .dark class on <html>.

const STORAGE_KEY = 'cascade-ui2-theme'
const mode = ref(readStored())
const media = window.matchMedia('(prefers-color-scheme: dark)')

function readStored() {
  const v = localStorage.getItem(STORAGE_KEY)
  return v === 'light' || v === 'dark' || v === 'auto' ? v : 'auto'
}

function resolved() {
  if (mode.value === 'auto') return media.matches ? 'dark' : 'light'
  return mode.value
}

function apply() {
  document.documentElement.classList.toggle('dark', resolved() === 'dark')
}

watch(mode, (m) => {
  localStorage.setItem(STORAGE_KEY, m)
  apply()
})

// React to OS changes while in auto mode.
media.addEventListener('change', () => {
  if (mode.value === 'auto') apply()
})

// Apply once on module load so there is no flash of the wrong theme.
apply()

export function useTheme() {
  function cycle() {
    mode.value = mode.value === 'light' ? 'dark' : mode.value === 'dark' ? 'auto' : 'light'
  }
  return { mode, resolved, cycle }
}

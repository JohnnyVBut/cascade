import { ref, onMounted, onUnmounted } from 'vue'

// Generic polling composable: calls fetcher() immediately, then every
// intervalMs. Exposes reactive data/error/loading and stops on unmount.
// Skips overlapping calls if a previous fetch is still in flight.
export function usePolling(fetcher, intervalMs = 5000, initial = null) {
  const data = ref(initial)
  const error = ref(null)
  const loading = ref(true)
  let timer = null
  let inFlight = false

  async function tick() {
    if (inFlight) return
    inFlight = true
    try {
      data.value = await fetcher()
      error.value = null
    } catch (e) {
      error.value = e.message || String(e)
    } finally {
      loading.value = false
      inFlight = false
    }
  }

  onMounted(() => {
    tick()
    timer = setInterval(tick, intervalMs)
  })
  onUnmounted(() => {
    if (timer) clearInterval(timer)
  })

  return { data, error, loading, refresh: tick }
}

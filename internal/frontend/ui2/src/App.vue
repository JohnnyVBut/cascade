<script setup>
import { RouterLink, RouterView } from 'vue-router'
import { useTheme } from './composables/useTheme.js'

const { mode, cycle } = useTheme()

const themeIcon = { light: '☀', dark: '☾', auto: '◐' }
const themeLabel = { light: 'Light', dark: 'Dark', auto: 'Auto' }
</script>

<template>
  <div style="min-height:100vh; background:var(--bg); color:var(--text);">
    <header style="border-bottom:1px solid var(--border);">
      <nav style="max-width:1100px; margin:0 auto; display:flex; align-items:center; gap:20px; padding:14px 24px;">
        <span style="font-size:16px; font-weight:500; letter-spacing:-0.01em;">Cascade</span>
        <div style="display:flex; gap:4px;">
          <RouterLink to="/" class="nav-link">Dashboard</RouterLink>
          <RouterLink to="/interfaces" class="nav-link">Interfaces</RouterLink>
        </div>
        <button
          @click="cycle"
          :title="'Theme: ' + themeLabel[mode]"
          aria-label="Toggle theme"
          style="margin-left:auto; width:32px; height:32px; border-radius:var(--radius); border:1px solid var(--border-strong); background:transparent; color:var(--text-secondary); cursor:pointer; font-size:15px;"
        >{{ themeIcon[mode] }}</button>
      </nav>
    </header>

    <main style="max-width:1100px; margin:0 auto; padding:24px;">
      <RouterView />
    </main>
  </div>
</template>

<style scoped>
.nav-link {
  padding: 6px 12px;
  border-radius: var(--radius);
  font-size: 13px;
  font-weight: 500;
  color: var(--text-secondary);
  text-decoration: none;
  transition: background 0.12s, color 0.12s;
}
.nav-link:hover {
  background: var(--surface-hover);
  color: var(--text);
}
.router-link-exact-active.nav-link {
  background: var(--accent-soft-bg);
  color: var(--accent-soft-fg);
}
</style>

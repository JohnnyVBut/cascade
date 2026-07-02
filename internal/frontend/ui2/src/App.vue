<script setup>
import { RouterLink, RouterView } from 'vue-router'
</script>

<template>
  <div class="min-h-screen bg-neutral-950 text-neutral-100">
    <header class="border-b border-neutral-800">
      <nav class="mx-auto flex max-w-6xl items-center gap-6 px-6 py-4">
        <span class="text-lg font-semibold tracking-tight">Cascade</span>
        <div class="flex gap-1">
          <RouterLink
            to="/"
            class="rounded-md px-3 py-1.5 text-sm font-medium text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-100"
            active-class="bg-neutral-800 text-neutral-100"
          >Dashboard</RouterLink>
          <RouterLink
            to="/interfaces"
            class="rounded-md px-3 py-1.5 text-sm font-medium text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-100"
            active-class="bg-neutral-800 text-neutral-100"
          >Interfaces</RouterLink>
        </div>
      </nav>
    </header>

    <main class="mx-auto max-w-6xl px-6 py-8">
      <RouterView />
    </main>
  </div>
</template>

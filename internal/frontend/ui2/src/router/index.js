import { createRouter, createWebHistory } from 'vue-router'
import { resolveRouterBase } from './base.js'
import DashboardView from '../views/DashboardView.vue'
import InterfacesView from '../views/InterfacesView.vue'

// History mode with a base resolved at runtime from the current path, so it
// includes any Caddy reverse-proxy prefix (/<ADMIN_PATH>/ui2/) in production.
// Deep links (e.g. /ui2/interfaces) resolve on both direct load (Fiber's
// NotFoundFile serves index.html) and client-side navigation.
const router = createRouter({
  history: createWebHistory(resolveRouterBase(window.location.pathname)),
  routes: [
    { path: '/', name: 'dashboard', component: DashboardView },
    { path: '/interfaces', name: 'interfaces', component: InterfacesView },
  ],
})

export default router

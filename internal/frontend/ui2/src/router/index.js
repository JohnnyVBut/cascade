import { createRouter, createWebHistory } from 'vue-router'
import DashboardView from '../views/DashboardView.vue'
import InterfacesView from '../views/InterfacesView.vue'

// History mode with an explicit base matching the /ui2/ mount point. Deep links
// (e.g. /ui2/interfaces) resolve on both direct load (Fiber's NotFoundFile
// serves index.html) and client-side navigation.
const router = createRouter({
  history: createWebHistory('/ui2/'),
  routes: [
    { path: '/', name: 'dashboard', component: DashboardView },
    { path: '/interfaces', name: 'interfaces', component: InterfacesView },
  ],
})

export default router

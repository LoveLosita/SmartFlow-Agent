import { createRouter, createWebHistory } from 'vue-router'

import { useAuthStore } from '@/stores/auth'
import AuthView from '@/views/AuthView.vue'
import AssistantView from '@/views/AssistantView.vue'
import DashboardView from '@/views/DashboardView.vue'
import ScheduleView from '@/views/ScheduleView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      redirect: '/dashboard',
    },
    {
      path: '/auth',
      name: 'auth',
      component: AuthView,
      meta: {
        guestOnly: true,
      },
    },
    {
      path: '/dashboard',
      name: 'dashboard',
      component: DashboardView,
      meta: {
        requiresAuth: true,
      },
    },
    {
      path: '/assistant/:id?',
      name: 'assistant',
      component: AssistantView,
      meta: {
        requiresAuth: true,
      },
    },
    {
      path: '/schedule',
      name: 'schedule',
      component: ScheduleView,
      meta: {
        requiresAuth: true,
      },
    },
  ],
})

router.beforeEach((to) => {
  const authStore = useAuthStore()

  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    return {
      name: 'auth',
      query: {
        redirect: to.fullPath,
      },
    }
  }

  if (to.meta.guestOnly && authStore.isAuthenticated) {
    return {
      name: 'dashboard',
    }
  }

  return true
})

export default router

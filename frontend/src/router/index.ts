import { createRouter, createWebHistory } from 'vue-router'

import { useAuthStore } from '@/stores/auth'
import AuthView from '@/views/AuthView.vue'
import AssistantView from '@/views/AssistantView.vue'
import DashboardView from '@/views/DashboardView.vue'

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
      path: '/assistant',
      name: 'assistant',
      component: AssistantView,
      meta: {
        requiresAuth: true,
      },
    },
  ],
})

router.beforeEach((to) => {
  const authStore = useAuthStore()

  // 1. 进入受保护页面前，必须先确认 access token 是否存在。
  // 2. 当前阶段只做“是否登录”的前端兜底，不在这里做 token 过期解析。
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

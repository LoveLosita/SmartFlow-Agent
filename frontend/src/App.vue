<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import MainSidebar from '@/components/common/MainSidebar.vue'

const route = useRoute()

const showLayout = computed(() => {
  return ['dashboard', 'assistant', 'schedule', 'forum', 'store', 'plan-detail'].includes(route.name as string)
})

// 全局加载进度条逻辑
const isLoading = ref(false)
const progress = ref(0)
let progressTimer: any = null

const startLoading = () => {
  isLoading.value = true
  progress.value = 0
  if (progressTimer) clearInterval(progressTimer)
  progressTimer = setInterval(() => {
    if (progress.value < 90) {
      progress.value += Math.random() * 10
    }
  }, 200)
}

const finishLoading = () => {
  progress.value = 100
  setTimeout(() => {
    isLoading.value = false
    progress.value = 0
    if (progressTimer) clearInterval(progressTimer)
  }, 300)
}

// 监听路由变化模拟进度条
import { useRouter } from 'vue-router'
const router = useRouter()
router.beforeEach((to, from, next) => {
  if (to.path !== from.path) startLoading()
  next()
})
router.afterEach(() => {
  finishLoading()
})
</script>

<template>
  <div v-if="isLoading" class="global-progress-bar" :style="{ width: progress + '%' }"></div>
  <div v-if="showLayout" class="smartmate-layout">
    <MainSidebar />
    <div class="smartmate-content">
      <router-view v-slot="{ Component }">
        <component :is="Component" />
      </router-view>
    </div>
  </div>
  <router-view v-else />
</template>

<style>
/* Reset base styles */
body {
  margin: 0;
}

.global-progress-bar {
  position: fixed;
  top: 0;
  left: 0;
  height: 3px;
  background: linear-gradient(to right, #3b82f6, #60a5fa);
  z-index: 9999;
  transition: width 0.3s ease;
  box-shadow: 0 0 8px rgba(59, 130, 246, 0.6);
}

.smartmate-layout {
  height: 100vh;
  height: 100dvh;
  box-sizing: border-box;
  padding: 10px;
  background: #f8fafc; /* Unified Flat Modern background */
  display: flex;
  gap: 10px;
  align-items: stretch;
  overflow: hidden;
  font-family: 'Inter', -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
}

/* 全局自定义滚动条样式 */
::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}

::-webkit-scrollbar-track {
  background: transparent;
}

::-webkit-scrollbar-thumb {
  background: rgba(15, 23, 42, 0.08);
  border-radius: 10px;
  transition: background 0.3s;
}

::-webkit-scrollbar-thumb:hover {
  background: rgba(15, 23, 42, 0.15);
}

/* Firefox 兼容性 */
* {
  scrollbar-width: thin;
  scrollbar-color: rgba(15, 23, 42, 0.08) transparent;
}

.smartmate-content {
  flex: 1;
  min-width: 0;
  min-height: 0;
  position: relative;
  display: flex;
  flex-direction: column;
}
</style>

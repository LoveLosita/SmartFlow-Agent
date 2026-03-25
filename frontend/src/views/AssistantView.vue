<script setup lang="ts">
import { computed } from 'vue'
import { ElMessage } from 'element-plus'
import { useRoute, useRouter } from 'vue-router'

import AssistantPanel from '@/components/dashboard/AssistantPanel.vue'

interface SidebarItem {
  key: 'home' | 'task' | 'calendar' | 'ai'
  label: string
  short: string
  to?: '/dashboard' | '/assistant' | '/schedule'
}

const router = useRouter()
const route = useRoute()

const sidebarItems: SidebarItem[] = [
  { key: 'home', label: '总览', short: '总', to: '/dashboard' },
  { key: 'task', label: '任务', short: '任' },
  { key: 'calendar', label: '日程', short: '程', to: '/schedule' },
  { key: 'ai', label: '助手', short: 'AI', to: '/assistant' },
]

const activeSidebarKey = computed<SidebarItem['key']>(() => {
  if (route.path.startsWith('/assistant')) {
    return 'ai'
  }
  if (route.path.startsWith('/schedule')) {
    return 'calendar'
  }
  return 'home'
})

function handleSidebarNavigate(item: SidebarItem) {
  // 1. 和首页保持相同行为：已接通路由直接跳转，未接通入口给出明确提示。
  // 2. 同路由不重复 push，避免产生冗余导航记录。
  // 3. 这样可保证两个页面的侧栏交互预期完全一致。
  if (item.to) {
    if (route.path !== item.to) {
      void router.push(item.to)
    }
    return
  }

  ElMessage.info(`${item.label} 页面正在开发中`)
}
</script>

<template>
  <main class="assistant-view">
    <section class="assistant-view__layout">
      <aside class="dashboard-sidebar">
        <div class="dashboard-sidebar__brand">S</div>
        <nav class="dashboard-sidebar__nav">
          <button
            v-for="item in sidebarItems"
            :key="item.key"
            type="button"
            class="dashboard-sidebar__nav-item"
            :class="{ 'dashboard-sidebar__nav-item--active': item.key === activeSidebarKey }"
            @click="handleSidebarNavigate(item)"
          >
            <span>{{ item.short }}</span>
            <small>{{ item.label }}</small>
          </button>
        </nav>
        <button type="button" class="dashboard-sidebar__settings">设</button>
      </aside>

      <AssistantPanel class="assistant-view__panel" view-mode="standalone" />
    </section>
  </main>
</template>

<style scoped>
.assistant-view {
  height: 100vh;
  padding: 10px;
  overflow: hidden;
  background: #f4f7fb;
}

.assistant-view__layout {
  height: calc(100vh - 20px);
  min-height: 0;
  display: grid;
  grid-template-columns: 78px minmax(0, 1fr);
  gap: 8px;
  align-items: stretch;
}

.dashboard-sidebar {
  height: 100%;
  border-radius: 26px;
  background: linear-gradient(180deg, #165ca8 0%, #104d8f 100%);
  padding: 16px 12px;
  display: grid;
  grid-template-rows: auto 1fr auto;
  gap: 16px;
}

.dashboard-sidebar__brand,
.dashboard-sidebar__settings {
  width: 50px;
  height: 50px;
  border: none;
  border-radius: 16px;
  background: rgba(255, 255, 255, 0.14);
  color: #fff;
  font-weight: 800;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.dashboard-sidebar__nav {
  display: grid;
  gap: 12px;
  align-content: start;
}

.dashboard-sidebar__nav-item {
  width: 54px;
  border: none;
  border-radius: 16px;
  background: transparent;
  color: rgba(255, 255, 255, 0.74);
  padding: 10px 8px;
  display: grid;
  justify-items: center;
  gap: 5px;
  cursor: pointer;
}

.dashboard-sidebar__nav-item span {
  width: 32px;
  height: 32px;
  border-radius: 11px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: rgba(255, 255, 255, 0.08);
  font-weight: 700;
}

.dashboard-sidebar__nav-item small {
  font-size: 10px;
}

.dashboard-sidebar__nav-item--active {
  background: rgba(255, 255, 255, 0.08);
  color: #fff;
}

.assistant-view__panel {
  min-width: 0;
  min-height: 0;
  height: 100%;
  border-radius: 18px;
}

@media (max-width: 980px) {
  .assistant-view__layout {
    height: auto;
    grid-template-columns: 1fr;
  }

  .dashboard-sidebar {
    height: auto;
    grid-template-columns: auto 1fr auto;
    grid-template-rows: none;
    align-items: center;
  }

  .dashboard-sidebar__nav {
    grid-auto-flow: column;
    justify-content: center;
  }
}

@media (max-width: 720px) {
  .assistant-view {
    height: auto;
    padding: 8px;
    overflow: visible;
  }
}
</style>

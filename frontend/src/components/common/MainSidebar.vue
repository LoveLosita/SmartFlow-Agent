<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

interface SidebarItem {
  key: 'home' | 'task' | 'calendar' | 'ai' | 'forum' | 'store'
  label: string
  short: string
  to?: '/dashboard' | '/assistant' | '/schedule' | '/forum' | '/store'
}

const sidebarItems: SidebarItem[] = [
  { key: 'home', label: '总览', short: '总', to: '/dashboard' },
  { key: 'calendar', label: '日程', short: '程', to: '/schedule' },
  { key: 'ai', label: '助手', short: 'AI', to: '/assistant' },
  { key: 'forum', label: '社区', short: '区', to: '/forum' },
  { key: 'store', label: '商店', short: '商', to: '/store' },
]

const route = useRoute()
const router = useRouter()

const activeSidebarKey = computed<SidebarItem['key']>(() => {
  if (route.path.startsWith('/assistant')) return 'ai'
  if (route.path.startsWith('/schedule')) return 'calendar'
  if (route.path.startsWith('/forum')) return 'forum'
  if (route.path.startsWith('/store')) return 'store'
  return 'home'
})

const activeSidebarIndex = computed(() => {
  return sidebarItems.findIndex(item => item.key === activeSidebarKey.value)
})

const activeIndicatorStyle = computed(() => {
  // 每个项高度 60px + 间隔 12px = 72px
  return {
    transform: `translateY(${activeSidebarIndex.value * 72}px)`
  }
})

function handleSidebarNavigate(item: SidebarItem) {
  if (item.to) {
    if (route.path !== item.to) void router.push(item.to)
    return
  }
  ElMessage.info(`${item.label} 页面正在开发中`)
}
</script>

<template>
  <aside class="dashboard-sidebar">
    <div class="dashboard-sidebar__brand">S</div>
    <nav class="dashboard-sidebar__nav">
      <div class="dashboard-sidebar__nav-indicator" :style="activeIndicatorStyle" />
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
</template>

<style scoped>
.dashboard-sidebar {
  width: 78px;
  height: 100%;
  border-radius: 24px;
  background: #0f172a;
  padding: 24px 12px;
  display: flex;
  flex-direction: column;
  gap: 32px;
  flex-shrink: 0;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05);
}

.dashboard-sidebar__brand {
  width: 48px;
  height: 48px;
  border-radius: 14px;
  background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
  color: #fff;
  font-size: 20px;
  font-weight: 800;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 4px 10px rgba(37, 99, 235, 0.3);
}

.dashboard-sidebar__nav { position: relative; display: grid; gap: 12px; align-content: start; }

.dashboard-sidebar__nav-indicator {
  position: absolute;
  left: 0;
  top: 0;
  width: 100%;
  height: 60px;
  background: rgba(255, 255, 255, 0.08);
  border-radius: 16px;
  transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  pointer-events: none;
  z-index: 0;
}

.dashboard-sidebar__nav-item {
  width: 100%;
  height: 60px;
  border: none;
  border-radius: 16px;
  background: transparent;
  color: rgba(255, 255, 255, 0.45);
  padding: 8px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  cursor: pointer;
  position: relative;
  z-index: 1;
  transition: color 0.2s;
}

.dashboard-sidebar__nav-item span {
  width: 32px;
  height: 32px;
  border-radius: 11px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  font-weight: 700;
  font-size: 14px;
  transition: all 0.2s;
}

.dashboard-sidebar__nav-item:hover { color: #f8fafc; }

.dashboard-sidebar__nav-item small { font-size: 10px; font-weight: 500; opacity: 0.8; }

.dashboard-sidebar__nav-item--active { color: #fff; }
.dashboard-sidebar__nav-item--active span { background: #3b82f6; box-shadow: 0 4px 12px rgba(59, 130, 246, 0.35); }

.dashboard-sidebar__settings {
  width: 48px;
  height: 48px;
  border: none;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.05);
  color: #94a3b8;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s;
  margin-top: auto;
}

.dashboard-sidebar__settings:hover { background: rgba(255, 255, 255, 0.1); color: #f8fafc; }
</style>

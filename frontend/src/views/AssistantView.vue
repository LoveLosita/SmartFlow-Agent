<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import AssistantPanel from '@/components/dashboard/AssistantPanel.vue'

interface PageSwitchItem {
  key: 'dashboard' | 'assistant'
  label: string
  short: string
  to: '/dashboard' | '/assistant'
}

const router = useRouter()
const route = useRoute()

const switchItems: PageSwitchItem[] = [
  {
    key: 'dashboard',
    label: '排程',
    short: '排',
    to: '/dashboard',
  },
  {
    key: 'assistant',
    label: '对话',
    short: 'AI',
    to: '/assistant',
  },
]

const activeSwitchKey = computed<PageSwitchItem['key']>(() =>
  route.path.startsWith('/assistant') ? 'assistant' : 'dashboard',
)

function handlePageSwitch(targetPath: PageSwitchItem['to']) {
  if (route.path !== targetPath) {
    router.push(targetPath)
  }
}
</script>

<template>
  <main class="assistant-view">
    <section class="assistant-view__layout">
      <aside class="assistant-view__switch-rail glass-panel">
        <button
          v-for="item in switchItems"
          :key="item.key"
          type="button"
          class="assistant-view__switch-item"
          :class="{ 'assistant-view__switch-item--active': activeSwitchKey === item.key }"
          @click="handlePageSwitch(item.to)"
        >
          <span>{{ item.short }}</span>
          <small>{{ item.label }}</small>
        </button>
      </aside>

      <AssistantPanel class="assistant-view__panel" view-mode="standalone" />
    </section>
  </main>
</template>

<style scoped>
.assistant-view {
  height: 100vh;
  padding: 12px;
  overflow: hidden;
  background: linear-gradient(180deg, #f6f8fb 0%, #eef2f7 100%);
}

.assistant-view__layout {
  height: calc(100vh - 24px);
  min-height: 0;
  display: grid;
  grid-template-columns: minmax(56px, 0.3fr) minmax(0, 6fr);
  gap: 12px;
}

.assistant-view__switch-rail {
  min-height: 0;
  border-radius: 18px;
  border: 1px solid rgba(15, 23, 42, 0.08);
  background: linear-gradient(180deg, rgba(249, 250, 252, 0.95), rgba(243, 247, 252, 0.98));
  box-shadow: 0 8px 20px rgba(15, 23, 42, 0.06);
  padding: 14px 7px;
  display: grid;
  align-content: start;
  gap: 8px;
}

.assistant-view__switch-item {
  border: none;
  border-radius: 12px;
  background: transparent;
  color: #6b7789;
  padding: 10px 4px 9px;
  cursor: pointer;
  display: grid;
  justify-items: center;
  gap: 4px;
}

.assistant-view__switch-item span {
  width: 32px;
  height: 32px;
  border-radius: 10px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 700;
  background: rgba(86, 101, 126, 0.1);
}

.assistant-view__switch-item small {
  font-size: 11px;
  line-height: 1;
}

.assistant-view__switch-item--active {
  color: #335fc2;
  background: linear-gradient(180deg, rgba(88, 126, 224, 0.16), rgba(88, 126, 224, 0.08));
}

.assistant-view__switch-item--active span {
  background: rgba(58, 95, 184, 0.2);
}

.assistant-view__panel {
  min-width: 0;
  min-height: 0;
  border-radius: 18px;
}
</style>

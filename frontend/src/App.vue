<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import MainSidebar from '@/components/common/MainSidebar.vue'

const route = useRoute()

const showLayout = computed(() => {
  return ['dashboard', 'assistant', 'schedule'].includes(route.name as string)
})
</script>

<template>
  <div v-if="showLayout" class="smartflow-layout">
    <MainSidebar />
    <div class="smartflow-content">
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

.smartflow-layout {
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

.smartflow-content {
  flex: 1;
  min-width: 0;
  min-height: 0;
  position: relative;
  display: flex;
  flex-direction: column;
}
</style>

<script setup lang="ts">
import AssistantPanel from '@/components/dashboard/AssistantPanel.vue'
</script>

<template>
  <AssistantPanel class="assistant-view__panel" view-mode="standalone" :initial-history-width="248" />
</template>

<style scoped>
.assistant-view__panel {
  min-width: 0;
  min-height: 0;
  height: 100%;
  border-radius: 24px;
  background: #ffffff;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.05);
  overflow: hidden;
}

@media (max-width: 720px) {
  .assistant-view__panel {
    height: auto;
    min-height: 100vh;
    min-height: 100svh;
  }
}
</style>

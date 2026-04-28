<script setup lang="ts">
import AssistantPanelDebug from './AssistantPanelDebug.vue'
</script>

<template>
  <div class="assistant-debug-page">
    <AssistantPanelDebug class="assistant-debug-panel" view-mode="standalone" />
  </div>
</template>

<style scoped>
.assistant-debug-page {
  width: 100vw;
  height: 100vh;
  background: #f0f2f5;
  display: flex;
  align-items: center;
  justify-content: center;
}

.assistant-debug-panel {
  width: 100%;
  height: 100%;
}
</style>

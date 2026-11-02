<template>
  <div class="h-full pb-20 lg:pb-0">
    <router-view />
    <!-- R123: 功能页共享底部 Tab Bar(与 Layout.vue 同款, 桌面端组件内自隐藏) -->
    <MobileTabBar />
  </div>
</template>

<script setup lang="ts">
import MobileTabBar from '@/components/MobileTabBar.vue'
</script>

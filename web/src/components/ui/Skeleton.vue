<template>
  <div class="skeleton" :class="[rounded, 'bg-bg-hover/60']" :style="{ height, width }" />
</template>

<script setup lang="ts">
// 骨架屏占位: 加载时闪烁渐变, 替代生硬的"加载中..."文字
withDefaults(defineProps<{
  height?: string
  width?: string
  rounded?: string
}>(), {
  height: '1rem',
  width: '100%',
  rounded: 'rounded-lg',
})
</script>

<style scoped>
.skeleton {
  position: relative;
  overflow: hidden;
}
.skeleton::after {
  content: '';
  position: absolute;
  inset: 0;
  transform: translateX(-100%);
  /* P2-38: 原白色高光在浅色主题下不可见——改用 currentColor 低透明度,
     深浅主题都有可见的扫光 */
  background: linear-gradient(90deg, transparent, color-mix(in srgb, currentColor 18%, transparent), transparent);
  animation: skeleton-shimmer 1.4s infinite;
}
@keyframes skeleton-shimmer {
  100% { transform: translateX(100%); }
}
</style>

<template>
  <!-- Phase 8 异常页: 404——此前路由无 catchAll, 访问不存在的路径渲染空白 -->
  <main class="min-h-[70vh] flex flex-col items-center justify-center gap-4 px-6 text-center">
    <div class="w-16 h-16 rounded-full bg-danger/10 flex items-center justify-center">
      <FileQuestion class="w-8 h-8 text-danger" stroke-width="1.75" />
    </div>
    <h1 class="text-xl font-semibold text-text-primary">页面不存在</h1>
    <p class="text-sm text-text-muted max-w-sm">您访问的地址有误或页面已被移除，请检查链接或返回首页。</p>
    <div class="flex gap-3 mt-2">
      <button class="px-4 py-2 rounded-lg bg-cyan-glow/20 text-cyan hover:bg-cyan-glow/30 transition-colors" @click="$router.back()">返回上一页</button>
      <router-link to="/dashboard" class="px-4 py-2 rounded-lg border border-border text-text-secondary hover:text-text-primary transition-colors">回到仪表盘</router-link>
    </div>
  </main>
</template>

<script setup lang="ts">
import { FileQuestion } from 'lucide-vue-next'
</script>

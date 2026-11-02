<template>
  <!-- 内部换 TDesign Breadcrumb 芯, 对外接口不变 -->
  <t-breadcrumb class="mb-4">
    <t-breadcrumb-item :to="homePath">首页</t-breadcrumb-item>
    <template v-if="!items || items.length === 0">
      <t-breadcrumb-item>{{ currentTitle }}</t-breadcrumb-item>
    </template>
    <template v-else>
      <t-breadcrumb-item v-for="(item, index) in items" :key="item.path" :to="index < items.length - 1 ? item.path : undefined">
        {{ item.label }}
      </t-breadcrumb-item>
    </template>
  </t-breadcrumb>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()

defineProps<{
  items?: Array<{ path: string; label: string }>
}>()

const currentTitle = computed(() => (route.meta.title as string) || '')
// P2-37: 原 startsWith('/admin') 会把 /adminx 之类误判为管理端。锚定路径边界。
const homePath = computed(() => /^\/admin(\/|$)/.test(route.path) ? '/admin/dashboard' : '/dashboard')
</script>

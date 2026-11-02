<template>
  <div class="flex flex-col items-center justify-center py-12 gap-4">
    <div class="w-8 h-8 rounded-full border-[4px] border-border border-t-cyan animate-spin" />
    <span v-if="text" class="text-sm text-text-secondary font-medium">{{ text }}</span>
  </div>
</template>

<script setup lang="ts">
defineProps<{ text?: string }>()
</script>

<template>
  <Modal v-model="confirmState.visible" :title="confirmState.title" :z-index="3000">
    <div class="flex items-start gap-3">
      <AlertTriangle class="w-6 h-6 text-amber shrink-0 mt-0.5" />
      <div>
        <p class="text-text-primary">{{ confirmState.message }}</p>
        <p v-if="confirmState.detail" class="text-sm text-text-secondary mt-2">{{ confirmState.detail }}</p>
      </div>
    </div>
    <template #footer>
      <button class="modal-btn-ghost" @click="settle(false)">{{ confirmState.cancelText }}</button>
      <button
        :class="confirmState.danger ? 'modal-btn-danger' : 'modal-btn-primary'"
        @click="settle(true)"
      >
        {{ confirmState.confirmText }}
      </button>
    </template>
  </Modal>
</template>

<script setup lang="ts">
import { AlertTriangle } from 'lucide-vue-next'
import Modal from './Modal.vue'
import { confirmState, settle } from '@/utils/confirm'
</script>

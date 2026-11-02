<template>
  <Modal v-model="visible" :title="title">
    <div class="flex items-start gap-3">
      <AlertTriangle class="w-6 h-6 text-amber shrink-0 mt-0.5" />
      <div>
        <p class="text-text-primary">{{ displayMessage }}</p>
        <p v-if="detail" class="text-sm text-text-secondary mt-2">{{ detail }}</p>
      </div>
    </div>
    <template #footer>
      <button @click="visible = false" class="modal-btn-ghost">
        {{ cancelText }}
      </button>
      <button
        @click="confirm"
        class="modal-btn-danger"
        :class="{ 'modal-btn-primary': !danger }"
      >
        {{ confirmText }}
      </button>
    </template>
  </Modal>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { AlertTriangle } from 'lucide-vue-next'
import Modal from './Modal.vue'

const props = withDefaults(defineProps<{
  modelValue?: boolean
  title?: string
  message: string
  detail?: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
}>(), {
  title: '确认',
  confirmText: '确认',
  cancelText: '取消',
})

const emit = defineEmits<{
  (e: 'confirm'): void
  (e: 'update:modelValue', v: boolean): void
}>()
const displayMessage = computed(() => props.message)
const localVisible = ref(false)
// 同时支持 v-model 和命令式 open() 两种用法：
// get 必须合并 props.modelValue，否则外部 v-model 置 true 时弹窗不显示
// （accounts/List.vue 的删除账号确认框即此用法，此前点删除无任何反应）。
const visible = computed({
  get: () => props.modelValue || localVisible.value,
  set: v => { localVisible.value = v; emit("update:modelValue", v) }
})

function open() { localVisible.value = true }
// P1-22 审计修复: 原 confirm 只清 localVisible——v-model 用法(modelValue 绑 true)
// 下 get 仍返回 true, 弹窗不关。双写: 本地态 + update:modelValue 一起清。
function confirm() {
  localVisible.value = false
  emit('update:modelValue', false)
  emit('confirm')
}

defineExpose({ open })
</script>

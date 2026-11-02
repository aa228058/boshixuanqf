# 博士轩群发器 · 模态框设计规范 v1.0

> 本规范适用于项目内所有模态框（Modal / Dialog / 底部抽屉）。
> 所有新弹窗必须遵守；存量弹窗逐步迁移到本规范。

---

## 1. 总原则

1. **唯一入口**：所有模态框必须基于 `@/components/ui/Modal.vue` 封装实现。
   该组件内部使用 `vue-final-modal`，对外提供统一的 `v-model / title / 默认插槽 / footer 插槽` 接口。
2. **禁止自绘**：不允许在业务页面里手写 `fixed inset-0` + 遮罩 + 卡片的弹窗结构。
   此类实现会导致层级失控、主题不同源、移动端不适配三大问题。
3. **确认类弹窗**：删除/危险操作确认统一用 `@/utils/confirm` 的 `ask()`（基于 ConfirmDialog）。

## 2. 结构规范

```
┌──────────────────────────────┐
│         ·· 把手 ··            │  移动端显示，桌面端隐藏
│  标题              [关闭 ✕]   │
│ ───────────────────────────  │
│                              │
│        内容区（可滚动）        │
│                              │
│ ───────────────────────────  │
│     [次按钮]  [主按钮]         │  移动端纵向堆叠，桌面端横向排列
└──────────────────────────────┘
```

## 3. 尺寸与布局

| 项 | 移动端（≤768px） | 桌面端（>768px） |
|---|---|---|
| 宽度 | 100% 占满 | max-width: 520px |
| 高度上限 | 92dvh | 88vh |
| 位置 | 底部抽屉（贴底） | 屏幕居中 |
| 圆角 | 仅顶部 20px | 四边 20px |
| 把手 | 40×4px 圆角灰条 | 不显示 |
| 水平安全边距 | 内容区 20px | 卡片外 24px |

## 4. 视觉规范

### 4.1 颜色（全部取项目 CSS 变量，深浅主题自动切换）

| 用途 | 变量 | 说明 |
|---|---|---|
| 弹窗背景 | `--td-bg-color-container` | 深浅主题各自适配 |
| 标题文字 | `--td-text-color-primary` | 18px / 700 |
| 正文/次级 | `--td-text-color-secondary` | 13-14px |
| 遮罩 | `rgba(0,0,0,0.45)` + `backdrop-filter: blur(4px)` | 毛玻璃效果 |
| 分隔线 | `rgba(128,128,128,0.10)` | footer 顶部 |
| 主按钮 | 项目主色渐变（紫 `#6b5bff` → `#8b7bff`） | 白字 |
| 次按钮 | 透明底 + 1px 边框 | 次级文字色 |

### 4.2 字体

| 元素 | 字号 | 字重 |
|---|---|---|
| 弹窗标题 | 18px | 700 |
| 内容区正文 | 14px | 400 |
| 按钮文字 | 15px | 500 |

## 5. 按钮规范

footer 内按钮统一使用规范 class（定义在全局样式，见 §9）：

| class | 用途 | 样式 |
|---|---|---|
| `.modal-btn-primary` | 主操作（保存/创建/确认） | 紫渐变底、白字、全宽（移动） |
| `.modal-btn-ghost` | 次操作（取消/关闭） | 透明底、边框、次级色文字 |

规则：
- 移动端 footer 按钮**纵向堆叠、全宽、高 46px**
- 桌面端 footer 按钮**横向排列、右对齐、高 36px**
- 主按钮永远排在次按钮之后（视觉动线从左到右 / 从上到下）
- 危险操作（删除）用红色系 `.modal-btn-danger`

## 6. 层级（z-index）规范

| 层级 | 用途 |
|---|---|
| 2500 + n×20 | 弹窗基准层，n = 打开顺序（后开的在上） |
| 3000 | ConfirmDialog（确认弹窗永远最顶） |
| 9999 | 全局 toast / 错误横幅 |

## 7. 动画规范

| 阶段 | 时长 | 缓动 |
|---|---|---|
| 进入 | 280ms | `cubic-bezier(0.16, 1, 0.3, 1)` |
| 退出 | 220ms | `cubic-bezier(0.4, 0, 0.2, 1)` |
| 位移 | 从 `translateY(100%)` → `0` | 底部滑入 |

遮罩淡入淡出与内容动画同步，不做过度炫技的动效。

## 8. 交互规范

- 点击遮罩关闭：开（`click-to-close`）
- ESC 键关闭：开（`esc-to-close`）
- 背景滚动锁定：开（`lock-scroll`）
- 内容超高：内容区内部滚动，footer 固定吸底
- 关闭后销毁内容：开（`destroy-on-close`，由 vue-final-modal 保证）

## 8.1 底部安全区

footer `padding-bottom = calc(20px + env(safe-area-inset-bottom, 0px))`。
iPhone 刘海/底部横条机型上按钮不被系统手势条遮挡。已在 Modal.vue 落地。

## 8.2 键盘弹起适配

H5 弹窗内点输入框时原生键盘弹起，底部抽屉按钮容易被顶出屏幕。

实现：Modal.vue 监听 `window.visualViewport` 的 resize/scroll，键盘高度
（`window.innerHeight - vv.height - vv.offsetTop`）写入 CSS 变量
`--modal-kb`；移动端 `.modal-card` 用 `translateY(calc(-1 * var(--modal-kb)))`
整体上移同样距离；弹窗关闭/卸载时清零并移除监听。

**验收**：手机弹窗内点输入框，footer 按钮仍在可视区内。

## 8.3 滚动指示

长表单滚动时用户可能不知道上方还有内容。footer 顶部加
`box-shadow: 0 -4px 12px rgba(0,0,0,0.04)` 的向上阴影，视觉上提示
"上方还有内容，按钮一直在这里"。已在 Modal.vue `.modal-footer` 落地。

## 8.4 设计令牌解耦

所有弹窗颜色禁止直接使用 `--td-*` 变量（避免未来换 UI 库时连锁修改）。
统一走 `--modal-bg` / `--modal-text-primary` / `--modal-text-secondary`
三个自有令牌，在 `modal-system.css` 顶层做一层映射：

```css
:root {
  --modal-bg: var(--td-bg-color-container, #fff);
  --modal-text-primary: var(--td-text-color-primary, #1a1a2e);
  --modal-text-secondary: var(--td-text-color-secondary, #6b7280);
}
html.theme-dark {
  --modal-bg: #161a30;
  --modal-text-primary: #e8eaf6;
  --modal-text-secondary: #9ca3af;
}
```

将来替换 UI 库只改这一个映射文件，弹窗组件代码零改动。

## 9. 代码落地

### 9.1 标准用法

```vue
<Modal v-model="showCreate" title="新建任务">
  <!-- 表单内容 -->
  <div class="space-y-4">
    <label class="block text-sm text-text-secondary">任务名称</label>
    <input v-model="name" class="w-full px-4 py-2.5 rounded-xl border border-border bg-bg-card" />
  </div>

  <template #footer>
    <button class="modal-btn-ghost" @click="showCreate = false">取消</button>
    <button class="modal-btn-primary" @click="submit">创建任务</button>
  </template>
</Modal>
```

### 9.2 危险确认

```ts
import { ask } from '@/utils/confirm'

const ok = await ask({ title: '删除任务', message: '删除后不可恢复', danger: true })
if (ok) { /* 执行删除 */ }
```

### 9.3 新增弹窗 Checklist

- [ ] 使用 `Modal.vue` 封装，不自绘
- [ ] 有标题（`title` prop）
- [ ] footer 用规范按钮 class
- [ ] 内容区不超 92dvh（超出会自动滚动）
- [ ] 深浅主题各看一遍

## 10. 迁移状态

| 弹窗 | 位置 | 状态 |
|---|---|---|
| 新建群发任务 | Broadcast.vue | ✅ 已规范 |
| 新建采集任务 | Scrape.vue | ✅ 已规范 |
| 新建加群任务 | Join.vue | ✅ 已规范 |
| 新建循环任务 | scheduled-tasks | ✅ 已规范 |
| 群组选择器 | GroupSelector.vue | ✅ 已规范 |
| 目标选择器 | TargetSelector.vue | ✅ 已规范 |
| 添加规则 | Monitor.vue | ✅ 已规范 |
| 添加账号 / 验证码 | Layout.vue | ✅ 已规范（v68.14 迁移） |
| 变量预览 | Broadcast.vue | ✅ 已规范（v68.14 迁移） |
| 底部安全区 / 键盘适配 / 滚动指示 / 设计令牌 | 全局 | ✅ 已落地（§8.1-§8.4） |
| 删除确认 | confirm.ts | ✅ 已规范 |
| 公告 / 通知面板 | AnnouncementPanel / NotificationPanel | ✅ 已规范 |

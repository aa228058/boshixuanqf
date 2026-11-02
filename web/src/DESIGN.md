# TG-Sender Design System v1.0
## Taste Skill 定调文档 — Glassmorphism + Cyberpunk 混合风格

> 本文档用于约束 AI 后续所有前端修改，确保风格不跑偏。
> 安装: `npx skills add https://github.com/Leonxlnx/taste-skill --skill "design-taste-frontend"`

---

## 1. 设计哲学（Design Philosophy）

### 核心定位
- **风格**: 深色玻璃拟态（Glassmorphism）+ 受控霓虹（Controlled Cyberpunk）
- **气质**: 科技感、专业感、沉浸感 —— 不是"夜店蹦迪"，是"太空舱控制台"
- **原则**: 发光是点缀，不是主角；玻璃是基底，不是装饰

### 三旋钮设定
| 旋钮 | 档位 | 说明 |
|------|------|------|
| VARIANCE | **中高** | 明显偏离 AI 默认模板，但不过度猎奇 |
| MOTION | **适中** | 有意义的动效，拒绝无目的闪烁 |
| DENSITY | **标准** | 信息层级清晰，不拥挤也不空旷 |

---

## 2. 色彩系统（Color System）

### 背景层级（Background Hierarchy）
```
Layer 0 (最底层):  #01030d  — 页面底色，接近纯黑带极微蓝
Layer 1 (星空层):  Three.js Canvas / CSS 渐变 — 动态背景
Layer 2 (玻璃基底): rgba(10, 18, 42, 0.55) — 卡片/面板底色
Layer 3 (玻璃强调): rgba(6, 14, 35, 0.7)  — 弹窗/浮层
Layer 4 (交互态):   rgba(14, 26, 58, 0.7)  — hover 状态
```

### 主色（Accent Colors）
| 名称 | 色值 | 用途 |
|------|------|------|
| Cyan | #38bdf8 | 主强调色：按钮、高亮、进度 |
| Cyan-Bright | #7dd3fc | hover 状态、发光峰值 |
| Purple | #a78bfa | 副强调色：图表、特殊状态 |
| Emerald | #34d399 | 成功状态 |
| Rose | #fb7185 | 错误/危险状态 |
| Amber | #fbbf24 | 警告/待处理状态 |

### 文字色（Text Colors）
| 层级 | 色值 | 用途 |
|------|------|------|
| Primary | #f0f4f8 | 标题、重要文字 |
| Secondary | #94a3b8 | 正文、描述 |
| Muted | #64748b | 辅助信息、时间戳 |
| Inverse | #0a0e1a | 按钮上的文字（深色底） |

### 边框色（Border Colors）
| 状态 | 色值 |
|------|------|
| 默认 | rgba(255, 255, 255, 0.08) |
| Hover | rgba(56, 189, 248, 0.25) |
| 激活/选中 | rgba(56, 189, 248, 0.35) |
| 错误 | rgba(251, 113, 133, 0.3) |

---

## 3. 字体系统（Typography）

### 字体栈
```css
font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Display', 'SF Pro Text', 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
```

### 字号层级
| 用途 | 大小 | 字重 | 行高 | 附加 |
|------|------|------|------|------|
| 页面标题 | text-xl (20px) | font-semibold (600) | 1.3 | text-gradient-blue |
| 卡片标题 | text-lg (18px) | font-semibold (600) | 1.4 | — |
| 正文 | text-sm (14px) | font-normal (400) | 1.6 | text-secondary |
| 辅助文字 | text-xs (12px) | font-medium (500) | 1.5 | text-muted |
| 数据/数字 | text-sm | font-mono | 1.4 | tabular-nums |

### 文字效果规范
- **渐变文字**: 仅用于页面大标题，用 `text-gradient-blue`
- **发光文字**: 禁止滥用，仅用于特殊强调（如空状态提示）
- **文字阴影**: 默认关闭，hover 时可选微弱发光

---

## 4. 间距系统（Spacing）

### 基础单位: 4px
| Token | 值 | 用途 |
|-------|-----|------|
| space-1 | 4px | 图标与文字间距 |
| space-2 | 8px | 紧凑内边距 |
| space-3 | 12px | 标准内边距 |
| space-4 | 16px | 卡片内边距 |
| space-6 | 24px | 区块间距 |
| space-8 | 32px | 页面边距 |

### 圆角规范
| 元素 | 圆角 |
|------|------|
| 按钮 | rounded-lg (8px) |
| 卡片 | rounded-xl (12px) |
| 弹窗 | rounded-2xl (16px) |
| 标签/徽章 | rounded-full |
| 输入框 | rounded-lg (8px) |

---

## 5. 组件规范（Component Specs）

### 5.1 按钮（Buttons）

#### 主按钮（Primary / glow-btn）
```css
background: linear-gradient(135deg, #38bdf8 0%, #0ea5e9 40%, #0284c7 100%);
color: #fff;
font-weight: 600;
border-radius: 8px;
box-shadow: 0 4px 18px rgba(56, 189, 248, 0.4), 0 0 0 1px rgba(56, 189, 248, 0.2);
```
- hover: 亮度提升 + 阴影扩散 + 微上浮
- active: 下沉 + 阴影收缩

#### 次按钮（Secondary / ghost-btn）
```css
background: rgba(255, 255, 255, 0.04);
border: 1px solid rgba(255, 255, 255, 0.08);
color: #94a3b8;
```
- hover: 背景变 cyan/10，边框变 cyan/25，文字变 cyan

#### 危险按钮（Danger / glow-btn-rose）
- 仅在删除、断开连接等不可逆操作使用

### 5.2 卡片（Cards / apple-card）
```css
background: rgba(10, 18, 42, 0.55);
backdrop-filter: blur(24px) saturate(1.5);
border: 1px solid rgba(255, 255, 255, 0.07);
border-radius: 12px;
box-shadow: 0 8px 32px rgba(0, 0, 0, 0.35);
```
- hover: 边框变 cyan/20，阴影加深，微上浮 2px

### 5.3 输入框（Inputs / glow-input）
```css
background: rgba(6, 14, 35, 0.5);
border: 1px solid rgba(255, 255, 255, 0.06);
border-radius: 8px;
```
- focus: 边框变 cyan/35，外发光 3px cyan/8

### 5.4 侧边栏（Sidebar / sidebar-glass）
```css
background: rgba(5, 12, 30, 0.75);
backdrop-filter: blur(32px) saturate(1.6);
border-right: 1px solid rgba(56, 189, 248, 0.06);
```

### 5.5 导航栏（Navbar / apple-nav）
- 滚动前: 完全透明
- 滚动后: 毛玻璃 + 底部微光边框

### 5.6 状态标签（Status Badges）
| 状态 | 样式 |
|------|------|
| 运行中 | status-running: cyan 背景 + 呼吸动画 |
| 成功 | status-success: emerald 背景，无动画 |
| 失败 | status-failed: rose 背景，无动画 |
| 待处理 | status-pending: amber 背景 + 呼吸动画 |
| 已暂停 | status-paused: 灰色背景，无动画 |

---

## 6. 动效系统（Motion）

### 原则
- **有目的的动效**: 引导注意力、反馈状态、增强层级
- **禁止无意义闪烁**: 没有功能意义的发光/脉冲一律删除

### 缓动函数
```css
--ease-smooth: cubic-bezier(0.22, 1, 0.36, 1);   /* 默认过渡 */
--ease-bounce: cubic-bezier(0.34, 1.56, 0.64, 1); /* 弹性反馈 */
--ease-snap: cubic-bezier(0.16, 1, 0.3, 1);      /* 快速响应 */
```

### 时长规范
| 场景 | 时长 |
|------|------|
| 微交互（hover） | 200-250ms |
| 状态切换 | 300-350ms |
| 页面过渡 | 400-500ms |
| 持续动画（呼吸） | 2-3s |

### 允许的发光动效
- 按钮 hover: 阴影扩散（0.25s）
- 卡片 hover: 边框发光 + 上浮（0.35s）
- 状态标签（运行中/待处理）: 呼吸脉冲（2s 循环）
- 输入框 focus: 外发光扩散（0.25s）

### 禁止的发光动效
- 页面加载时的全屏闪烁
- 无目的的背景色脉冲
- 多个元素同时呼吸（最多 2 个）

---

## 7. 布局规范（Layout）

### 页面结构
```
[星空背景层 — 固定全屏]
  └── [App 容器]
        ├── [侧边栏 — 固定 256px]
        │     ├── Logo
        │     ├── 账号切换器
        │     ├── 导航菜单（分组：功能 / 记录 / 系统）
        │     └── 套餐信息 + 退出
        └── [主内容区]
              ├── [顶部导航栏 — 滚动时毛玻璃化]
              └── [页面内容 — p-6 md:p-8]
```

### 响应式断点
| 断点 | 行为 |
|------|------|
| < 768px | 侧边栏隐藏为抽屉，禁用 backdrop-filter，简化阴影 |
| 768px - 1024px | 侧边栏固定，内容区自适应 |
| > 1024px | 全功能展示，所有动效启用 |

---

## 8. 星空背景规范（Starry Sky）

### 视觉参数
| 参数 | 桌面端 | 移动端 |
|------|--------|--------|
| 星星数量 | 280 | 150 |
| 背景渐变 | 左上 #0d2d73 → 右下 #01040d | 简化渐变 |
| 流星频率 | 0.3%/帧 | 禁用 |
| 帧率 | 120fps | 60fps |

### 性能规则
- 使用 requestAnimationFrame
- 星星用 Points 粒子系统，不要用单独 Mesh
- 移动端禁用 Shader 复杂计算
- 页面不可见时暂停渲染

---

## 9. 反模式清单（Anti-Patterns）

### 绝对禁止
- [ ] **紫蓝渐变**: 不用默认 AI 紫（#8B5CF6 → #6366F1），用 cyan → blue
- [ ] **三列等宽卡片**: 用非对称布局或 Bento 网格
- [ ] **Inter 字体**: 用系统字体栈
- [ ] **破折号滥用**: 不用 "—" 做装饰，用竖线或留白分隔
- [ ] **居中 Hero**: 用左对齐或网格布局
- [ ] **廉价阴影**: 不用纯黑大阴影，用带颜色的柔和阴影
- [ ] **无意义动画**: 不用纯装饰性旋转/弹跳

### 需要审查
- [ ] 每个发光效果是否有功能意义
- [ ] 每个渐变是否有色彩逻辑
- [ ] 每个圆角是否一致
- [ ] 每个间距是否是 4 的倍数

---

## 10. 文件组织

```
web/src/
├── styles/
│   ├── tailwind.css      # 全局样式 + 自定义工具类
│   └── animations.css    # 关键帧动画
├── components/
│   ├── ui/               # 基础组件（按钮、卡片、输入框）
│   ├── forms/            # 表单组件
│   └── selectors/        # 选择器组件
├── views/
│   ├── Layout.vue        # 布局框架
│   ├── dashboard/        # 仪表盘
│   ├── workspace/        # 功能工作区
│   └── auth/             # 登录注册
└── DESIGN.md             # 本文档
```

---

## 11. 修改检查清单（AI 每次修改前必读）

Before making any UI change, verify:
1. [ ] 颜色是否来自本规范的色值表？
2. [ ] 间距是否是 4px 的倍数？
3. [ ] 圆角是否符合组件规范？
4. [ ] 动效是否"有目的"？
5. [ ] 移动端是否有降级方案？
6. [ ] 是否引入了新的硬编码色值？
7. [ ] 发光效果是否过度？

---

*Version: 1.0*
*Style: Glassmorphism + Controlled Cyberpunk*
*Tone: Professional, Immersive, Tech-forward*

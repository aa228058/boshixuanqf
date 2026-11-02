<template>
  <div class="max-w-3xl mx-auto px-4 md:px-8 py-6 space-y-6">
    <div>
      <h1 class="text-body font-semibold text-text-primary">更新日志</h1>
      <p class="text-sm text-text-muted mt-1">每个版本用户能直接用到的变化, 都在这里。</p>
    </div>

    <div class="glass rounded-[var(--t-r-md)] p-4 flex items-center gap-6">
      <div class="text-center">
        <div class="text-2xl font-bold text-cyan">{{ versions[0].version }}</div>
        <div class="text-xs text-text-muted mt-0.5">当前版本</div>
      </div>
      <div class="text-center">
        <div class="text-2xl font-bold text-text-primary">{{ versions.length }}</div>
        <div class="text-xs text-text-muted mt-0.5">累计版本</div>
      </div>
    </div>

    <div v-for="v in versions" :key="v.version" class="glass rounded-[var(--t-r-md)] p-5 lift">
      <div class="flex items-center gap-3 flex-wrap">
        <span class="font-bold text-text-primary">{{ v.version }}</span>
        <span class="text-xs px-2 py-0.5 rounded-full" :class="v.tag === '新功能' ? 'bg-cyan-glow/30 text-cyan' : 'bg-bg-primary text-text-muted'">{{ v.tag }}</span>
        <span class="text-xs text-text-muted ml-auto">{{ v.date }}</span>
      </div>
      <h3 class="font-medium text-text-primary mt-2">{{ v.title }}</h3>
      <ul class="mt-2.5 space-y-1.5">
        <li v-for="(item, i) in v.items" :key="i" class="text-sm text-text-secondary flex gap-2 leading-relaxed">
          <span class="text-cyan shrink-0">·</span>{{ item }}
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts">
const versions = [
  {
    version: 'v68.9', date: '2026-10-20', tag: '新功能',
    title: '账号体系大升级 + 全站 UI 焕新 + 统计报表',
    items: [
      '备用池上线: 备用号不占套餐额度, 转正才占用, 死号可一键转备用释放额度',
      '死号重连: 用已存登录态直接拉回在线, 不用重新接验证码',
      '每账号成功/失败计数: 账号卡片直接显示战绩, 任务点"统计"看分账号明细',
      '账号任务摘要: 账号行实时显示"执行中: 群发任务A"等徽标',
      '一键退出全部群组: 账号行按钮, 逐个退群带风控间隔, 结果明确反馈',
      '发送日历: 群发页新 tab, 月历热力图按天回看发送量',
      '群发循环化: 循环发送并入新建表单, 原独立页合并为页内 tab',
      '头像自动补拉: 老号上线自动补齐头像, 不再显示灰占位',
      '抽奖助手: 独立管理页, 套餐→租户→账号三层控制',
      '帮助中心: 新手上手/防封要点/进阶玩法/常见问题',
      'UI 全面升级: 页面过渡/按钮按压反馈/Toast 动画/卡片悬浮/深色模式修复等 20+ 项',
    ],
  },
  {
    version: 'v68.8', date: '2026-10-19', tag: '修复',
    title: '9 个关键缺陷修复(源码审查发现)',
    items: [
      '编队系统全接口 500(squads 列缺失)——已修',
      '定时/循环战役永不激活(campaigns 缺 run_at 列)——已修',
      '编辑分组清空成员(前端传空数组)——已修',
      '免费卡密激活后仍被判过期——已修',
      'Docker 构建失败(alpine 标签/npm 锁文件/编译错误/web/dist 漏拷)——已修',
      '顶栏透明导致内容重叠——已修',
      '管理端套餐管理全路由校验补齐——未勾功能真实 403',
    ],
  },
]
</script>

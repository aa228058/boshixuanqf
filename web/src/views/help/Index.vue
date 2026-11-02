<template>
  <div class="max-w-3xl mx-auto px-4 md:px-8 py-6 space-y-6">
    <div>
      <h1 class="text-body font-semibold text-text-primary">帮助中心</h1>
      <p class="text-sm text-text-muted mt-1">新手上路到进阶玩法, 看完这篇就能开始。</p>
    </div>

    <section v-for="sec in sections" :key="sec.title" class="glass rounded-[var(--t-r-md)] p-5 lift">
      <h2 class="font-semibold text-text-primary flex items-center gap-2">
        <span class="text-lg">{{ sec.icon }}</span>{{ sec.title }}
      </h2>
      <ol class="mt-3 space-y-2.5">
        <li v-for="(step, i) in sec.steps" :key="i" class="flex gap-2.5 text-sm text-text-secondary leading-relaxed">
          <span class="shrink-0 w-5 h-5 rounded-full bg-cyan-glow/40 text-cyan text-xs flex items-center justify-center font-medium">{{ i + 1 }}</span>
          <span v-html="step" />
        </li>
      </ol>
    </section>

    <section class="glass rounded-[var(--t-r-md)] p-5 border-cyan/20">
      <h2 class="font-semibold text-text-primary">❓ 常见问题</h2>
      <div class="mt-3 space-y-3">
        <details v-for="faq in faqs" :key="faq.q" class="group">
          <summary class="cursor-pointer text-sm text-text-primary font-medium list-none flex items-center justify-between">
            {{ faq.q }}
            <span class="text-text-muted group-open:rotate-90 transition-transform">›</span>
          </summary>
          <p class="mt-2 text-sm text-text-secondary leading-relaxed pl-1">{{ faq.a }}</p>
        </details>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
const sections = [
  {
    icon: '🚀', title: '快速上手(5 分钟跑通第一次群发)',
    steps: [
      '注册账号: 拿到卡密后在注册页输入, 用户名+卡密+密码即可开通',
      '添加账号: 「账号管理」→ 输入手机号 → TG 验证码登录(协议号走 Session 导入页)',
      '同步群组: 选中账号 → 「同步群组」把账号已加入的群拉进系统, 之后才能选群发目标',
      '创建群发: 「群发消息」→ 新建任务 → 选群组+写文案 → 设置间隔(建议 5-12 秒)→ 提交',
      '看进度: 任务列表实时滚动日志, 成功失败一目了然; 「发送日历」tab 按天回看战绩',
    ],
  },
  {
    icon: '🛡️', title: '防封要点(重要)',
    steps: [
      '<b>间隔别贪快</b>: 系统护栏会按账号等级自动抬升到安全区间, 手动设 1 秒也会被拉回来',
      '<b>新号先养</b>: 协议协同的养号机制会让新号满 7 天"毕业"再上战场, 别急着用新号轰炸',
      '<b>文案要混淆</b>: 开启 auto-vary 或 {{词典}} 变量, 千人千面的文案比千篇一律安全十倍',
      '<b>号死了别慌</b>: 点「重新连接」用存着的登录态直接拉回(不用验证码); 救不活的转备用号养养',
      '<b>失败率高就停</b>: 账号行有成功/失败计数, 失败率高的号转备用, 别让一个号拖垮一批号',
    ],
  },
  {
    icon: '🤖', title: '进阶玩法',
    steps: [
      '<b>循环群发</b>: 新建任务时勾选"循环发送", 设间隔分钟数, 每轮自动重随机文案',
      '<b>协同战役</b>: 协议号批量养号→加群→流水线群发, 阵亡自动拉替补',
      '<b>抽奖助手</b>: 设置页开启后自动抢群里抽奖, 可按账号逐个开关',
      '<b>发送日历</b>: 群发页第二个 tab, 每天发了多少条一目了然',
    ],
  },
]

const faqs = [
  { q: '为什么群发一直"排队中"?', a: '检查: ①账号是否在线(离线号不干活) ②套餐并发额度(企业版默认 40 并发, 免费版 1 并发, 超了就得排队) ③定时任务是到点才激活。' },
  { q: '号显示"受限"怎么办?', a: '账号行点「申诉」按钮, 系统自动访问 @SpamBot 提交申诉; 申诉期间号会冷却, 别重复点。申诉失败考虑换号。' },
  { q: '为什么发不出去一直 flood?', a: '触发 TG 限流了。加大发送间隔、给号挂代理(线路分散)、或让号休息几小时。风控是渐进的, 别硬刚。' },
  { q: '群发和循环群发有什么区别?', a: '现在是一个功能了——新建任务时勾选"循环发送"即可, 每轮结束按设定间隔自动开始下一轮, 文案自动重混淆。' },
  { q: '备用号占套餐额度吗?', a: '不占。转备用立即释放额度, 转正(启用)时才占用, 满额会提示你先升级套餐或转备用其他号。' },
  { q: '深色模式下页面有灰蒙蒙的一层?', a: '那是旧版本的全局背景网格问题, 新版本已修复——重新部署并强刷(Ctrl+Shift+R)即可。' },
]
</script>

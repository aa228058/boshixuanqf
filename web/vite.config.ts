import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { resolve } from 'path'
import { readFileSync } from 'fs'

const pkg = JSON.parse(readFileSync('./package.json', 'utf-8'))

// 注意: 不要加回 @vitejs/plugin-legacy。
// 它的 modern/legacy 探测脚本在 MIUI 等 Android webview 上会误判, 同时注入 SystemJS legacy chunk,
// 执行报错被浏览器归为跨域错误, 前端只能收到无信息的 "Script error."(红条查无元凶)。
// 项目客户端全是现代浏览器(Chrome 130+ / iOS 15+), modern build (es2020) 足够。
export default defineConfig({
  // 必须绝对路径: 相对路径在深层/异常 URL 下资源 404, 导致刷新白屏进不去
  base: '/',
  plugins: [vue(), tailwindcss()],
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version)
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src')
    }
  },
  server: {
    port: 3000,
    proxy: {
      '/api': 'http://localhost:8080',
      '/admin': 'http://localhost:8080'
    }
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    target: 'es2020',
    rollupOptions: {
      output: {
        manualChunks: {
          'echarts': ['echarts'],
          'vue-vendor': ['vue', 'vue-router', 'pinia']
        }
      }
    }
  }
})

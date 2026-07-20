<template>
  <n-config-provider :theme="themeRef" :theme-overrides="themeOverrides" :locale="zhCN" :date-locale="dateZhCN">
    <n-loading-bar-provider>
      <n-message-provider>
        <n-dialog-provider>
          <n-notification-provider>
            <router-view />
          </n-notification-provider>
        </n-dialog-provider>
      </n-message-provider>
    </n-loading-bar-provider>
  </n-config-provider>
</template>

<script setup>
import { computed } from 'vue'
import {
  NConfigProvider, NLoadingBarProvider, NMessageProvider,
  NDialogProvider, NNotificationProvider, zhCN, dateZhCN, darkTheme
} from 'naive-ui'
import { theme } from './store.js'

// 主题: 直接读取 store 内部 ref 做 computed,任何页面调用 theme.toggle() 都会立即生效。
// 浅色模式传 null(naive-ui 约定),深色模式传 darkTheme。
const themeRef = computed(() => theme.ref.value === 'dark' ? darkTheme : null)

// 智谱风格的科技蓝主题(浅色/深色通用覆盖)
const themeOverrides = {
  common: {
    primaryColor: '#3D6EFF',
    primaryColorHover: '#5A85FF',
    primaryColorPressed: '#2B57E6',
    borderRadius: '8px',
    fontFamily: "'Inter','PingFang SC','Microsoft YaHei',sans-serif"
  }
}
</script>

<template>
  <n-config-provider :theme-overrides="themeOverrides" :locale="zhCN" :date-locale="dateZhCN">
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
import {
  NConfigProvider, NLoadingBarProvider, NMessageProvider,
  NDialogProvider, NNotificationProvider, zhCN, dateZhCN
} from 'naive-ui'

// 智谱风格的科技蓝主题
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

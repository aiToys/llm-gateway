<template>
  <n-config-provider :theme-overrides="overrides" :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider><n-dialog-provider><n-notification-provider>
      <router-view />
    </n-notification-provider></n-dialog-provider></n-message-provider>
  </n-config-provider>
</template>
<script setup>
import { NConfigProvider, NMessageProvider, NDialogProvider, NNotificationProvider, zhCN, dateZhCN } from 'naive-ui'
const overrides = {
  common: {
    primaryColor: '#0F766E', primaryColorHover: '#15897f', primaryColorPressed: '#0b5d56',
    borderRadius: '8px',
    fontFamily: "'Inter','PingFang SC','Microsoft YaHei',sans-serif"
  }
}
</script>

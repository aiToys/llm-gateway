---
layout: home

hero:
  name: LLM Gateway
  text: 多租户 LLM 网关
  tagline: 统一 OpenAI / Anthropic 协议，对接百炼·火山方舟·千帆，负载均衡与计费内建
  image:
    src: /logo.svg
    alt: LLM Gateway logo
  actions:
    - theme: brand
      text: 快速开始
      link: /quickstart
    - theme: alt
      text: 核心概念
      link: /concepts

features:
  - title: 双协议接入
    details: 一套网关同时提供 OpenAI（/v1/chat/completions）与 Anthropic（/v1/messages）兼容接口，存量 SDK 零改动接入。
  - title: 多供应商负载均衡
    details: 一个逻辑模型挂多个供应商渠道，支持加权随机 / 轮询 / 主备 / 随机 / 固定渠道五种路由策略，自动故障转移。
  - title: 真实成本核算
    details: 售价绑定模型（面向用户统一价），成本绑定渠道（每家供应商独立单价），毛利按实际命中渠道精确核算。
  - title: 多租户 + BYOK
    details: 租户隔离的用量与账单，支持租户自带密钥（BYOK）覆盖平台默认渠道；租户可自助启停模型。
  - title: 预付计费与限流
    details: 整数分精度、按 token 实时扣费；Redis 分钟桶实现的 RPM/TPM 限流与渠道熔断，跨副本一致。
  - title: 控制面 / 数据面分离
    details: 同进程内嵌或拆分为独立 edge 二进制横向扩展；管理端 + 用户端 Vue 控制台开箱即用，支持明暗主题切换与移动端响应式。
---

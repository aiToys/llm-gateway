// Package common 提供 controller 共用的小工具。
package common

import "github.com/gin-gonic/gin"

// Error 以 OpenAI 风格返回错误:{"error":{"type":..,"message":..}}。
func Error(g *gin.Context, status int, typ, message string) {
	g.JSON(status, gin.H{
		"error": gin.H{
			"type":    typ,
			"message": message,
		},
	})
}

// AnthropicError 以 Anthropic 风格返回错误:顶层必须有 type:"error"。
// Anthropic SDK / Claude Code 严格要求 {"type":"error","error":{"type":..,"message":..}};
// 用 OpenAI 格式会导致客户端 JSON 解析异常、真实错误信息丢失。
func AnthropicError(g *gin.Context, status int, typ, message string) {
	g.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    typ,
			"message": message,
		},
	})
}

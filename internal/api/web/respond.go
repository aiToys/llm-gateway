package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aitoys/llm-gateway/internal/logging"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// okData 统一返回带数据的成功响应: {"data": ...}。
// 新接口应优先用此(而非 g.JSON(200, gin.H{"id":...}) 等顶层格式),保持前后端契约一致,
// 前端可统一 unwrap(body.data)。历史顶层响应(login 的 {token,...} / 操作类 {ok:true})保留。
func (s *Server) okData(g *gin.Context, data any) {
	g.JSON(http.StatusOK, gin.H{"data": data})
}

// ok 返回无数据的成功响应: {"ok": true}(用于 PUT/DELETE 等只需状态的操作)。
func (s *Server) ok(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

// respondInternal 统一处理 5xx 内部错误:完整 err 记服务端日志(含 request_id 供排障),
// 客户端只收到通用 "internal_error",避免把 PG SQLSTATE / 连接串片段 / 文件路径 / 堆栈
// 等内部结构泄露给调用方(原直接 g.JSON(... err.Error()) 有 50+ 处泄露点)。
func (s *Server) respondInternal(g *gin.Context, err error) {
	logging.L().Warn("internal error",
		"req_id", g.GetString("request_id"),
		"path", g.Request.URL.Path,
		"err", err.Error())
	g.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
}

// bindJSON 绑定 JSON 请求并把校验错误转为客户端友好的中文提示。
// 成功返回 true;失败已写 400 响应(脱敏:不暴露 Go 结构路径与 validator 内部信息,
// 如原 "Key: 'createChannelReq.Name' Error:Field validation for 'Name' failed on the 'required' tag")。
// 用法: if !s.bindJSON(g, &req) { return }
func (s *Server) bindJSON(g *gin.Context, req any) bool {
	if err := g.ShouldBindJSON(req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": friendlyBindErr(err)})
		return false
	}
	return true
}

// friendlyBindErr 把 validator / JSON 解析错误转为中文提示,字段名保留(便于客户端定位),
// 但去除 Go 结构路径与 validator tag 原文。
func friendlyBindErr(err error) string {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		for _, fe := range ve {
			field := fe.Field()
			switch fe.Tag() {
			case "required":
				return field + " 不能为空"
			case "oneof":
				return field + " 取值无效"
			case "email":
				return field + " 格式无效(需为邮箱)"
			case "min", "max", "len", "gte", "lte":
				return field + " 长度/范围不符合要求"
			default:
				return field + " 格式无效"
			}
		}
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "invalid character"), strings.Contains(msg, "unexpected end of JSON"), strings.Contains(msg, "EOF"):
		return "请求体格式无效(JSON 解析失败)"
	case strings.Contains(msg, "cannot unmarshal"):
		return "请求体字段类型不匹配"
	default:
		return "请求参数无效"
	}
}


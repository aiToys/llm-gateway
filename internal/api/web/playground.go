package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/gin-gonic/gin"
)

// playgroundChat 用户端聊天台(非流式)。使用 JWT 主体鉴权,内部复用 relay。
func (s *Server) playgroundChat(g *gin.Context) {
	var req canon.Request
	if !s.bindJSON(g, &req) {
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		g.JSON(http.StatusBadRequest, gin.H{"error": "model and messages required"})
		return
	}
	req.Stream = false
	sub := mustSub(g)
	resp, meta, err := s.Relay.Chat(g.Request.Context(), sub, &req)
	if err != nil {
		g.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	g.Header("X-Request-Id", meta.RequestID)
	g.Set("rl_tokens", int64(meta.Usage.TotalTokens))
	g.JSON(http.StatusOK, resp)
}

// playgroundChatStream 用户端聊天台(流式 SSE)。
func (s *Server) playgroundChatStream(g *gin.Context) {
	var req canon.Request
	if !s.bindJSON(g, &req) {
		return
	}
	req.Stream = true
	sub := mustSub(g)
	ch, meta, err := s.Relay.ChatStream(g.Request.Context(), sub, &req)
	if err != nil {
		g.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	g.Header("Content-Type", "text/event-stream")
	g.Header("Cache-Control", "no-cache")
	g.Header("Connection", "keep-alive")
	g.Header("X-Request-Id", meta.RequestID)
	g.Stream(func(w io.Writer) bool {
		select {
		case chunk, ok := <-ch:
			if !ok {
				fmt.Fprint(w, "data: [DONE]\n\n")
				g.Writer.Flush()
				return false
			}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", b)
			g.Writer.Flush()
			return true
		case <-g.Request.Context().Done():
			return false
		}
	})
}

package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type createRedeemReq struct {
	AmountCents int64  `json:"amount_cents" binding:"required"`
	Count       int    `json:"count"`
	Note        string `json:"note"`
}

// adminCreateRedeemCodes 平台 admin 批量生成充值卡密(1-1000 张/次)。
// 卡密是平台发行的付款凭证,仅平台超管可创建。
func (s *Server) adminCreateRedeemCodes(g *gin.Context) {
	var req createRedeemReq
	if !s.bindJSON(g, &req) || req.AmountCents <= 0 {
		g.JSON(http.StatusBadRequest, gin.H{"error": "amount_cents must be positive"})
		return
	}
	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > 1000 {
		g.JSON(http.StatusBadRequest, gin.H{"error": "单次最多生成 1000 张"})
		return
	}
	now := time.Now()
	codes := make([]string, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		code := genRedeemCode()
		if err := s.Store.CreateRedeemCode(g.Request.Context(), &model.RedeemCode{
			ID: uuid.NewString(), Code: code, AmountCents: req.AmountCents,
			Status: "active", Note: req.Note, CreatedAt: now,
		}); err != nil {
			s.respondInternal(g, err)
			return
		}
		codes = append(codes, code)
	}
	s.audit(g, "admin.redeem_create", fmt.Sprintf("%d codes x %d cents", len(codes), req.AmountCents))
	g.JSON(http.StatusOK, gin.H{"codes": codes})
}

// adminListRedeemCodes 平台 admin 查看卡密列表(含使用情况)。
func (s *Server) adminListRedeemCodes(g *gin.Context) {
	codes, err := s.Store.ListRedeemCodes(g.Request.Context(), 200)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": codes})
}

// genRedeemCode 生成 16 位大写卡密(4-4-4-4 分段,便于人工抄录/口述)。
// 基于 uuid 去横线取前 16 位,碰撞概率可忽略(且 code 唯一索引兜底,冲突时 CreateRedeemCode 报错)。
func genRedeemCode() string {
	raw := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))
	s := raw[:16]
	return s[:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/aitoys/llm-gateway/internal/store"
	"github.com/gin-gonic/gin"
)

func parseAggParams(g *gin.Context, scope, scopeID string) store.AggParams {
	groupBy := g.DefaultQuery("group_by", "model")
	switch groupBy {
	case "model", "provider", "api_key":
	default:
		groupBy = "model"
	}
	bucket := g.DefaultQuery("bucket", "day")
	switch bucket {
	case "minute", "hour", "day":
	default:
		bucket = "day"
	}
	days, _ := strconv.Atoi(g.DefaultQuery("days", "30"))
	if days <= 0 || days > 365 {
		days = 30
	}
	to := time.Now()
	from := to.AddDate(0, 0, -days)
	return store.AggParams{Scope: scope, ScopeID: scopeID, GroupBy: groupBy, Bucket: bucket, From: from, To: to}
}

func (s *Server) usageAggregate(g *gin.Context) {
	sub := mustSub(g)
	rows, err := s.Store.Aggregate(g.Request.Context(), parseAggParams(g, "user", sub.UserID))
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": rows})
}

func (s *Server) adminUsageAggregate(g *gin.Context) {
	scope := g.DefaultQuery("scope", "all")
	rows, err := s.Store.Aggregate(g.Request.Context(), parseAggParams(g, scope, g.Query("scope_id")))
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": rows})
}

package web

import (
	"errors"
	"net/http"

	"github.com/aitoys/llm-gateway/internal/files"
	"github.com/gin-gonic/gin"
)

// playgroundUpload 用户端聊天台上传文件(JWT 鉴权),返回可访问 URL。
// 上传后前端把 URL 作为 image_url content-part 加入消息。
// 大小/MIME 校验由 files.Upload 统一兜底(见 MaxUploadBytes / allowedUploadMIME)。
func (s *Server) playgroundUpload(g *gin.Context) {
	if s.FileSvc == nil {
		g.JSON(http.StatusServiceUnavailable, gin.H{"error": "file service disabled"})
		return
	}
	g.Request.Body = http.MaxBytesReader(g.Writer, g.Request.Body, files.MaxUploadBytes+1<<20)
	sub := mustSub(g)
	fh, err := g.FormFile("file")
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "file required (max 20MB)"})
		return
	}
	src, err := fh.Open()
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "cannot read file"})
		return
	}
	defer src.Close()
	f, err := s.FileSvc.Upload(g.Request.Context(), sub.TenantID, sub.UserID, fh.Filename, fh.Header.Get("Content-Type"), src)
	if err != nil {
		switch {
		case errors.Is(err, files.ErrTooLarge):
			g.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large (max 20MB)"})
		case errors.Is(err, files.ErrUnsupportedType):
			g.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "unsupported file type"})
		default:
			g.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed"})
		}
		return
	}
	g.JSON(http.StatusOK, gin.H{
		"id": f.ID, "filename": f.Filename, "bytes": f.Size,
		"url": f.StorageURL, "mime_type": f.MimeType,
	})
}

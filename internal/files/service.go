// Package files 提供文件托管(本地存储,接口可扩展 S3/MinIO)。
package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/aitoys/llm-gateway/internal/store"
	"github.com/google/uuid"
)

var ErrNotFound = errors.New("file not found")

// MaxUploadBytes 单文件上传上限(20MB)。在 io.Copy 期间以 LimitReader 强制,
// 防止客户端谎报 Content-Length 或流式塞满磁盘。
const MaxUploadBytes = 20 << 20

// allowedUploadMIME 允许上传的 MIME 白名单。聚焦多模态/文档场景;其余类型拒绝。
var allowedUploadMIME = map[string]struct{}{
	"image/png":       {},
	"image/jpeg":      {},
	"image/webp":      {},
	"image/gif":       {},
	"application/pdf": {},
	"text/plain":      {},
}

// ErrTooLarge 超出上传上限。
var ErrTooLarge = fmt.Errorf("file exceeds %d bytes", MaxUploadBytes)

// ErrUnsupportedType MIME 不在白名单。
var ErrUnsupportedType = errors.New("unsupported file type")

// Service 文件托管服务。
type Service struct {
	Store   *store.Store
	Root    string
	BaseURL string
}

func New(s *store.Store, root, baseURL string) (*Service, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Service{Store: s, Root: root, BaseURL: baseURL}, nil
}

// Upload 保存上传文件,返回文件元信息(含可访问 URL)。
// 校验: ① MIME 必须在白名单; ② 大小不超过 MaxUploadBytes(LimitReader 兜底,防谎报)。
func (s *Service) Upload(ctx context.Context, tenantID, userID, filename, contentType string, r io.Reader) (*model.File, error) {
	if !isAllowedMIME(contentType) {
		return nil, ErrUnsupportedType
	}
	id := uuid.NewString()
	ext := filepath.Ext(filename)
	objName := fmt.Sprintf("%s/%s/%s%s", tenantID, userID, id, ext)
	if ext == "" {
		// 用 mime 补扩展
		if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
			objName += exts[0]
		}
	}
	full := filepath.Join(s.Root, objName)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(full) //nolint:gosec // objName=tenantID/userID/uuid.ext 全服务端拼接,filename 仅取扩展名
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// 多读 1 字节以探测是否超限: LimitReader 在 MaxUploadBytes+1 处截断,若读满则视为过大。
	lr := io.LimitReader(r, MaxUploadBytes+1)
	n, err := io.Copy(f, lr)
	if err != nil {
		_ = os.Remove(full)
		return nil, err
	}
	if n > MaxUploadBytes {
		_ = os.Remove(full)
		return nil, ErrTooLarge
	}
	url := strings.TrimRight(s.BaseURL, "/") + "/files/" + objName
	m := &model.File{
		ID: id, TenantID: tenantID, UserID: userID, Filename: filename,
		MimeType: contentType, Size: n, StorageURL: objName, Purpose: "vision", CreatedAt: time.Now(),
	}
	if err := s.Store.CreateFile(ctx, m); err != nil {
		_ = os.Remove(full)
		return nil, err
	}
	m.StorageURL = url
	return m, nil
}

// isAllowedMIME 校验声明的 MIME 是否在白名单;空 contentType 拒绝。
func isAllowedMIME(ct string) bool {
	ct = strings.TrimSpace(strings.Split(ct, ";")[0])
	if ct == "" {
		return false
	}
	_, ok := allowedUploadMIME[ct]
	return ok
}

// Get 取文件元信息。
func (s *Service) Get(ctx context.Context, id string) (*model.File, error) {
	f, err := s.Store.GetFile(ctx, id)
	if err != nil {
		return nil, ErrNotFound
	}
	return f, nil
}

// FilePath 返回本地磁盘路径(供下载)。
func (s *Service) FilePath(objName string) string {
	// 防目录穿越: 清洗路径。
	clean := filepath.Clean("/" + objName)
	return filepath.Join(s.Root, filepath.Base(filepath.Dir(clean)), filepath.Base(clean))
}

// ServeHTTP 提供文件下载(静态)。
func (s *Service) ServeHTTP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/files/")
		full := filepath.Join(s.Root, rel)
		if !strings.HasPrefix(filepath.Clean(full), filepath.Clean(s.Root)) {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(full) //nolint:gosec // 上方 HasPrefix 校验已限定在 Root 内,无穿越风险
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		stat, _ := f.Stat()
		if stat.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, full, stat.ModTime(), f)
	}
}

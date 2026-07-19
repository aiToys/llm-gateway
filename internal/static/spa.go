// Package static 提供单页应用(SPA)的静态托管与 history 回退。
package static

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// MountSPAs 把多个 SPA 挂载到指定前缀。若 dir 不存在则跳过(开发态前端独立 dev server)。
// 命中规则: 前缀下的真实文件直接返回;否则回退到该 SPA 的 index.html(history 模式)。
func MountSPAs(r *gin.Engine, apps map[string]string) {
	type app struct{ prefix, dir string }
	var list []app
	for prefix, dir := range apps {
		if _, err := os.Stat(dir); err != nil {
			continue // dist 未构建,跳过
		}
		list = append(list, app{prefix: prefix, dir: dir})
		// 暴露 assets 等静态文件
		r.Static(prefix+"/assets", filepath.Join(dir, "assets"))
	}
	if len(list) == 0 {
		return
	}
	// 按前缀长度降序,确保 /admin 优先于 / 匹配。
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if len(list[j].prefix) > len(list[i].prefix) {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		// API/接入点前缀不应落到 SPA 回退,返回 404 而非 index.html(避免误导)
		for _, reserved := range []string{"/api/", "/v1/", "/files/", "/healthz"} {
			if strings.HasPrefix(p, reserved) {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
		}
		for _, a := range list {
			if a.prefix == "/" || strings.HasPrefix(p, a.prefix) {
				// 先尝试 dist 根下的真实静态文件(logo.svg / favicon 等 public 资源);
				// 命中则直接返回,避免被 history 回退误当路由 → index.html(导致 <img> 加载到 HTML)。
				if rel := strings.TrimPrefix(strings.TrimPrefix(p, a.prefix), "/"); rel != "" {
					if fp, ok := safeFile(a.dir, rel); ok {
						c.File(fp)
						return
					}
				}
				serveIndex(c, a.dir)
				return
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})
}

// safeFile 返回 dir 下 rel 对应的路径(若存在且为文件且未越界 dir)。
// 防 .. 路径穿越:Clean 后绝对路径必须仍位于 dir 之内。
func safeFile(dir, rel string) (string, bool) {
	cleaned := filepath.Clean("/" + rel) // 绝对化,消除 ../
	fp := filepath.Join(dir, cleaned)
	absDir, err1 := filepath.Abs(dir)
	absFp, err2 := filepath.Abs(fp)
	if err1 != nil || err2 != nil || !strings.HasPrefix(absFp+string(filepath.Separator), absDir+string(filepath.Separator)) {
		return "", false
	}
	fi, err := os.Stat(fp)
	if err != nil || fi.IsDir() {
		return "", false
	}
	return fp, true
}

func serveIndex(c *gin.Context, dir string) {
	indexPath := filepath.Join(dir, "index.html")
	c.File(indexPath)
}

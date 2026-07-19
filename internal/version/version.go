// Package version 暴露构建版本信息(经 -ldflags 注入;缺省为开发态占位)。
package version

// Version 程序版本。构建时注入: -ldflags "-X github.com/aitoys/llm-gateway/internal/version.Version=$(cat VERSION)"
var Version = "0.1.0-dev"

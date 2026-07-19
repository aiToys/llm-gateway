// Package apikey 生成与解析 API Key。
package apikey

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/aitoys/llm-gateway/internal/crypto"
)

const prefix = "sk-"

// Generate 生成 sk-<随机> 格式的 API Key 与其 hash。
func Generate() (plain, hash, keyPrefix string, err error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", err
	}
	plain = prefix + hex.EncodeToString(b)
	hash = crypto.APIKeyHash(plain)
	if len(plain) >= 10 {
		keyPrefix = plain[:10]
	}
	return plain, hash, keyPrefix, nil
}

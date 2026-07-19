package apikey

import (
	"strings"
	"testing"

	"github.com/aitoys/llm-gateway/internal/crypto"
)

// TestGenerateFormat 验证生成密钥的格式契约: sk- 前缀 + hex 随机体 + 正确长度。
func TestGenerateFormat(t *testing.T) {
	plain, hash, keyPrefix, err := Generate()
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if !strings.HasPrefix(plain, "sk-") {
		t.Errorf("plain want prefix 'sk-', got %q", plain)
	}
	// 24 字节随机 → 48 hex 字符 + 3 前缀 = 51。
	if len(plain) != len(prefix)+48 {
		t.Errorf("plain length want %d, got %d", len(prefix)+48, len(plain))
	}
	// keyPrefix 为密钥前 10 位(用于列表展示,不泄露完整密钥)。
	if keyPrefix != plain[:10] {
		t.Errorf("keyPrefix want %q, got %q", plain[:10], keyPrefix)
	}
	if hash == "" {
		t.Error("hash must not be empty")
	}
}

// TestGenerateHashDeterministic 验证 hash 对同一明文确定: 同明文→同 hash,
// 这是鉴权侧用 hash 比对校验密钥的前提。
func TestGenerateHashDeterministic(t *testing.T) {
	plain, hash, _, err := Generate()
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if crypto.APIKeyHash(plain) != hash {
		t.Fatal("hash must be deterministic for the same plaintext")
	}
}

// TestGenerateUnique 验证随机性: 连续两次生成的明文必须不同(防碰撞/可预测)。
func TestGenerateUnique(t *testing.T) {
	a, _, _, err := Generate()
	if err != nil {
		t.Fatalf("first Generate error: %v", err)
	}
	b, _, _, err := Generate()
	if err != nil {
		t.Fatalf("second Generate error: %v", err)
	}
	if a == b {
		t.Fatal("two generated keys must differ")
	}
}

// TestGenerateHashNotEqualPlaintext 验证 hash 不是明文本身(防明文落库)。
func TestGenerateHashNotEqualPlaintext(t *testing.T) {
	plain, hash, _, err := Generate()
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if hash == plain {
		t.Fatal("hash must not equal plaintext")
	}
}

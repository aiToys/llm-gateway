// Package crypto 提供密码哈希、API Key 哈希、渠道密钥加密、随机串。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// HashPassword 用户密码 bcrypt。
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func VerifyPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// APIKeyHash 用 argon2id 对 API Key 做单向 hash(只存 hash)。
func APIKeyHash(key string) string {
	salt := []byte("llm-gateway-apikey-salt") // 固定 salt: Key 本身已是高熵随机串
	h := argon2.IDKey([]byte(key), salt, 1, 64*1024, 2, 32)
	return hex.EncodeToString(h)
}

// RandomHex 生成 n 字节的十六进制随机串。
func RandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// NewID 生成 16 字节 hex 标识符(32 字符),用作各实体的随机主键。
// crypto/rand.Read 在正常系统上极罕见失败;失败时回退到纳秒时间戳,保证非空与唯一。
// 统一各包 mustID/storeID/cryptoID 的兜底逻辑,消除逐字符复制的多份实现。
func NewID() string {
	id, err := RandomHex(16)
	if err != nil {
		return fmt.Sprintf("id-%d", time.Now().UnixNano())
	}
	return id
}

// --- 渠道密钥 AES-GCM ---

// Cipher 渠道密钥加解密器。
type Cipher struct{ gcm cipher.AEAD }

// NewCipher 用 hex(32 字节) 主密钥构造。
func NewCipher(masterHex string) (*Cipher, error) {
	key, err := hex.DecodeString(masterHex)
	if err != nil {
		return nil, fmt.Errorf("master key not hex: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("master key must be 32 bytes (64 hex chars)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{gcm: g}, nil
}

// Encrypt 加密明文,返回 base64(nonce||ciphertext)。
func (c *Cipher) Encrypt(plain string) (string, error) {
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := c.gcm.Seal(nil, nonce, []byte(plain), nil)
	out := append(nonce, ct...) //nolint:gocritic // AES-GCM 密文布局: nonce 作前缀拼密文,有意新建切片不赋回 nonce
	return base64.StdEncoding.EncodeToString(out), nil
}

func (c *Cipher) Decrypt(enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	ns := c.gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := c.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

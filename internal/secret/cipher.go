// Package secret 提供凭据的对称加密（AES-GCM）。
//
// 号池里的洛谷密码与 cookie 等同于登录态，落库前一律经本包加密：
// repository 负责在读写时调用 Seal/Open，其余各层只看到明文结构体，
// 不感知密文格式。
//
// 密文格式："v1:" + base64(nonce || ciphertext || tag)。版本前缀用于后续
// 轮换算法或密钥；密钥轮换流程见 README（用新密钥重新加密后再删除旧密钥）。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// 密文版本前缀
const (
	versionV1     = "v1:"
	currentPrefix = versionV1
)

// Cipher AES-GCM 加解密器，可并发使用
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher 用环境变量里提供的密钥构造加解密器。
//
// 密钥接受 hex（32/48/64 个 hex 字符）或 base64 编码，解码后必须是
// 16/24/32 字节（AES-128/192/256）。两种编码存在歧义时按 hex 优先解析，
// 因此建议用 `openssl rand -base64 32` 生成（44 字符，不会被当成 hex）。
func NewCipher(key string) (*Cipher, error) {
	raw, err := decodeKey(key)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("secret: 初始化 AES 失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: 初始化 GCM 失败: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Seal 加密明文；空字符串返回空字符串（表示"没有凭据"，与未设置同义）
func (c *Cipher) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if c == nil || c.aead == nil {
		return "", errors.New("secret: 加解密器未初始化")
	}

	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("secret: 生成 nonce 失败: %w", err)
	}

	sealed := c.aead.Seal(nil, nonce, []byte(plain), nil)
	buf := make([]byte, 0, len(nonce)+len(sealed))
	buf = append(buf, nonce...)
	buf = append(buf, sealed...)

	return currentPrefix + base64.StdEncoding.EncodeToString(buf), nil
}

// Open 解密 Seal 产生的密文；空字符串返回空字符串
func (c *Cipher) Open(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	if c == nil || c.aead == nil {
		return "", errors.New("secret: 加解密器未初始化")
	}

	body, ok := strings.CutPrefix(encoded, currentPrefix)
	if !ok {
		return "", fmt.Errorf("secret: 密文格式不受支持（缺少 %q 前缀）", currentPrefix)
	}

	data, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return "", fmt.Errorf("secret: 密文 base64 解码失败: %w", err)
	}

	nonceSize := c.aead.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("secret: 密文长度不足")
	}

	plain, err := c.aead.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("secret: 解密失败（密钥不匹配或数据被篡改）: %w", err)
	}
	return string(plain), nil
}

// decodeKey 解析密钥：hex 优先，其次 base64（标准与无填充两种）
func decodeKey(key string) ([]byte, error) {
	s := strings.TrimSpace(key)
	if s == "" {
		return nil, errors.New("secret: 密钥不能为空")
	}

	if isHex(s) {
		if raw, err := hex.DecodeString(s); err == nil && validKeyLen(len(raw)) {
			return raw, nil
		}
	}
	for _, dec := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if raw, err := dec.DecodeString(s); err == nil && validKeyLen(len(raw)) {
			return raw, nil
		}
	}

	return nil, errors.New("secret: 密钥需为 hex 或 base64 编码，且解码后为 16/24/32 字节")
}

func isHex(s string) bool {
	if len(s)%2 != 0 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return len(s) > 0
}

func validKeyLen(n int) bool {
	return n == 16 || n == 24 || n == 32
}

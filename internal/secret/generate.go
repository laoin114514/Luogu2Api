package secret

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// 本文件提供配置里两个"该用随机数、不该手写"的值的生成器。
//
// 不依赖 openssl：Windows 默认没有它，容器运行时镜像里也没有；而这两个值恰好是配置里
// 最不能将就的两项——ACCOUNT_SECRET_KEY 直接是落库凭据（洛谷密码 / cookie）的 AES 密钥，
// ADMIN_TOKEN 是 /api/v1 全部接口的唯一口令。服务入口的 -genkey 就是调这两个函数。

// keyBytes 是生成密钥/令牌使用的随机字节数：32 字节对应 AES-256，用于令牌也足够。
//
// NewCipher 也接受 16/24 字节（AES-128/192），这里统一生成最强的 32。
const keyBytes = 32

// GenerateKey 生成一个可直接用作 ACCOUNT_SECRET_KEY 的随机密钥（32 字节，base64 标准编码）。
//
// 用 base64 而不是 hex：与文档一贯推荐的 `openssl rand -base64 32` 同一种形式（44 字符，带
// "=" 填充），NewCipher 的解析顺序（hex 优先）不会把它误判成 hex。
func GenerateKey() (string, error) {
	raw := make([]byte, keyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("secret: 生成随机密钥失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// GenerateToken 生成一个随机令牌（32 字节，hex 编码，64 字符）。
//
// ADMIN_TOKEN 本身对格式没有任何要求（任意非空字符串都能用），这个函数给的是"拿不准就用
// 随机值"的默认选项。刻意用 hex 而不是 base64：不含 = + / -，粘进 .env、环境变量、
// curl -H 与 URL 里都不需要转义。
func GenerateToken() (string, error) {
	raw := make([]byte, keyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("secret: 生成随机令牌失败: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

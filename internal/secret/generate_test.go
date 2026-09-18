package secret

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// 生成的密钥必须能直接喂给 NewCipher：这是 -genkey 输出的唯一验收标准
func TestGenerateKeyIsUsableAsCipherKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		t.Fatalf("生成的不是合法 base64: %q: %v", key, err)
	}
	if len(raw) != 32 {
		t.Errorf("解码后长度 = %d 字节, want 32", len(raw))
	}
	// 要能原样粘进 .env / 环境变量，不能含空白或需要转义的字符
	if strings.ContainsAny(key, " \t\r\n\"'") {
		t.Errorf("密钥含需要转义的字符: %q", key)
	}

	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher(GenerateKey()): %v", err)
	}
	sealed, err := c.Seal("p@ssw0rd")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if got, err := c.Open(sealed); err != nil || got != "p@ssw0rd" {
		t.Errorf("Open = %q, %v", got, err)
	}
}

func TestGenerateKeyIsRandom(t *testing.T) {
	a, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	b, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if a == b {
		t.Error("两次生成的密钥相同")
	}
}

// 令牌是 hex：不含 = + / -，放进命令行与 .env 都不需要转义
func TestGenerateTokenIsPlainHex(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("令牌长度 = %d, want 64", len(token))
	}
	if _, err := hex.DecodeString(token); err != nil {
		t.Errorf("令牌不是合法 hex: %q: %v", token, err)
	}
	if strings.ContainsAny(token, "=+/\"' \t") {
		t.Errorf("令牌含需要转义的字符: %q", token)
	}

	other, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if token == other {
		t.Error("两次生成的令牌相同")
	}
}

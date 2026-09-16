package secret

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// 32 字节密钥，分别用 hex 与 base64 表示
var (
	key32    = strings.Repeat("ab", 32)
	key16    = strings.Repeat("cd", 16)
	rawKey32 = strings.Repeat("k", 32)
)

func mustCipher(t *testing.T, key string) *Cipher {
	t.Helper()
	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher(%q): %v", key, err)
	}
	return c
}

func TestSealOpenRoundTrip(t *testing.T) {
	c := mustCipher(t, key32)

	for _, plain := range []string{"p@ssw0rd", "中文密码", `[{"name":"_uid","value":"1965145"}]`, "a"} {
		sealed, err := c.Seal(plain)
		if err != nil {
			t.Fatalf("Seal(%q): %v", plain, err)
		}
		if !strings.HasPrefix(sealed, "v1:") {
			t.Errorf("Seal 结果应带版本前缀: %q", sealed)
		}
		// 只对足够长的明文断言"密文不含明文"：单字符会偶然出现在 base64 里
		if len(plain) >= 8 && strings.Contains(sealed, plain) {
			t.Errorf("密文不应包含明文: %q", sealed)
		}
		if sealed == plain {
			t.Errorf("密文不应等于明文: %q", sealed)
		}

		got, err := c.Open(sealed)
		if err != nil {
			t.Fatalf("Open(%q): %v", sealed, err)
		}
		if got != plain {
			t.Errorf("Open = %q, want %q", got, plain)
		}
	}
}

// 同一明文两次加密应产生不同密文（随机 nonce）
func TestSealNonceIsRandom(t *testing.T) {
	c := mustCipher(t, key32)

	a, err := c.Seal("same")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	b, err := c.Seal("same")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if a == b {
		t.Error("两次加密结果相同，nonce 未随机化")
	}
}

func TestEmptyValueStaysEmpty(t *testing.T) {
	c := mustCipher(t, key32)

	sealed, err := c.Seal("")
	if err != nil || sealed != "" {
		t.Errorf("Seal(\"\") = %q, %v；空值应保持空", sealed, err)
	}
	if got, err := c.Open(""); err != nil || got != "" {
		t.Errorf("Open(\"\") = %q, %v；空值应保持空", got, err)
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	c := mustCipher(t, key32)

	sealed, err := c.Seal("secret")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	body := strings.TrimPrefix(sealed, "v1:")
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw[len(raw)-1] ^= 0xff // 翻转最后一字节（tag）
	tampered := "v1:" + base64.StdEncoding.EncodeToString(raw)

	if _, err := c.Open(tampered); err == nil {
		t.Error("篡改后的密文必须解密失败")
	}
}

func TestOpenWrongKeyFails(t *testing.T) {
	sealed, err := mustCipher(t, key32).Seal("secret")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	other := mustCipher(t, key16)
	if _, err := other.Open(sealed); err == nil {
		t.Error("换密钥后必须解密失败")
	}
}

func TestOpenRejectsUnknownVersion(t *testing.T) {
	c := mustCipher(t, key32)

	for _, in := range []string{"v2:AAAA", "AAAA", "v1"} {
		if _, err := c.Open(in); err == nil {
			t.Errorf("Open(%q) 应报错", in)
		}
	}
}

func TestOpenRejectsShortPayload(t *testing.T) {
	c := mustCipher(t, key32)

	// 合法 base64，但短于 GCM nonce
	if _, err := c.Open("v1:" + base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Error("长度不足的密文应报错")
	}
}

func TestNewCipherKeyFormats(t *testing.T) {
	raw := []byte(rawKey32)

	tests := []struct {
		name string
		key  string
		ok   bool
	}{
		{"hex-32", key32, true},
		{"hex-16", key16, true},
		{"base64-32", base64.StdEncoding.EncodeToString(raw), true},
		{"base64-raw-32", base64.RawStdEncoding.EncodeToString(raw), true},
		{"empty", "", false},
		{"blank", "   ", false},
		{"too-short", "abc", false},
		{"bad-length", base64.StdEncoding.EncodeToString([]byte("12345")), false},
		{"non-encoded", "!!!not-a-key!!!", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCipher(tt.key)
			if tt.ok && err != nil {
				t.Errorf("NewCipher(%q) 应成功: %v", tt.key, err)
			}
			if !tt.ok && err == nil {
				t.Errorf("NewCipher(%q) 应失败", tt.key)
			}
		})
	}
}

// hex 与 base64 形式指向同一密钥时，密文应可互相解密
func TestKeyEncodingEquivalence(t *testing.T) {
	raw, err := hex.DecodeString(key32)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	byHex := mustCipher(t, key32)
	byBase64 := mustCipher(t, base64.StdEncoding.EncodeToString(raw))

	sealed, err := byHex.Seal("secret")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := byBase64.Open(sealed)
	if err != nil || got != "secret" {
		t.Errorf("Open = %q, %v", got, err)
	}
}

func TestNilCipherFailsSafely(t *testing.T) {
	var c *Cipher

	if _, err := c.Seal("x"); err == nil {
		t.Error("nil Cipher 的 Seal 应报错")
	}
	if _, err := c.Open("v1:AAAA"); err == nil {
		t.Error("nil Cipher 的 Open 应报错")
	}
	// 空值不需要密钥
	if got, err := c.Seal(""); err != nil || got != "" {
		t.Errorf("nil Cipher 处理空值不应报错: %q, %v", got, err)
	}
}

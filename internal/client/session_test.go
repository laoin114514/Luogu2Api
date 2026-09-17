package client

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/laoin114514/luogu2api/internal/config"
	"github.com/laoin114514/luogu2api/pkg/fakeuseragent"
)

// TestNewSDKFactoryTakesUserAgentPerSession 验证"每个会话一条 UA"。
//
// SDK 把 User-Agent 存在未导出字段里且没有 getter，无法从 client 上回读，
// 因此这里注入计数用的生成器，观察"每建一次会话取一次 UA、且每个会话取到的不同"。
func TestNewSDKFactoryTakesUserAgentPerSession(t *testing.T) {
	var taken []string
	newUA := func() string {
		ua := fmt.Sprintf("test-ua/%d", len(taken)+1)
		taken = append(taken, ua)
		return ua
	}

	factory := newSDKFactory(context.Background(), config.Luogu{Timeout: time.Second, Retry: 1}, newUA)

	for i := 0; i < 3; i++ {
		session, err := factory(nil)
		if err != nil {
			t.Fatalf("第 %d 次建会话失败: %v", i+1, err)
		}
		if session == nil {
			t.Fatalf("第 %d 次建会话返回 nil", i+1)
		}
	}

	if len(taken) != 3 {
		t.Fatalf("3 次建会话应当取 3 条 UA，实际 %d 条", len(taken))
	}
	seen := make(map[string]bool, len(taken))
	for i, ua := range taken {
		if want := fmt.Sprintf("test-ua/%d", i+1); ua != want {
			t.Errorf("第 %d 条 UA = %q，期望 %q", i+1, ua, want)
		}
		if seen[ua] {
			t.Errorf("第 %d 条 UA 与前面重复: %q", i+1, ua)
		}
		seen[ua] = true
	}
}

// TestNewSDKFactoryWithRealUserAgent 生产接线（fakeuseragent.Random）的冒烟测试：
// 生成器必须能正常建出会话，且给出的 UA 非空——空 UA 比"UA 不够好"更糟。
func TestNewSDKFactoryWithRealUserAgent(t *testing.T) {
	factory := newSDKFactory(context.Background(), config.Luogu{Timeout: time.Second}, fakeuseragent.Random)

	session, err := factory(nil)
	if err != nil {
		t.Fatalf("用真实 UA 生成器建会话失败: %v", err)
	}
	if session == nil {
		t.Fatal("建会话返回 nil")
	}
	if ua := fakeuseragent.Random(); ua == "" {
		t.Fatal("默认 UA 生成器返回空字符串")
	}
}

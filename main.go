// Command 2api 是 Luogu2Api 的 Web 服务入口。
//
// 依赖的洛谷客户端通过 go.mod 的 replace 指向仓库内的 pkg/luoguClient
// （git submodule，见 .gitmodules），因此修改 SDK 源码无需发版即可生效。
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	luogu "github.com/laoin114514/luoguClient"

	"github.com/laoin114514/luogu2api/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP 监听地址")
	cookieFile := flag.String("cookies", "cookies.json", "登录态 cookie 文件（不存在则以匿名身份运行）")
	flag.Parse()

	logger := log.New(os.Stderr, "[2api] ", log.LstdFlags)

	client, err := luogu.NewClient()
	if err != nil {
		logger.Fatalf("创建洛谷客户端失败: %v", err)
	}

	// cookie 由本服务自行持久化：SDK 只在内存中保存，是否落盘由调用方决定
	if data, err := os.ReadFile(*cookieFile); err == nil {
		if err := client.ImportCookies(data); err != nil {
			logger.Printf("恢复 cookie 失败，改为匿名运行: %v", err)
		} else if client.Auth.IsAuthenticated() {
			logger.Printf("已恢复登录态，UID=%d", client.UID())
		} else {
			logger.Printf("cookie 已失效，改为匿名运行")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		logger.Printf("读取 cookie 文件失败，改为匿名运行: %v", err)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.New(server.SDKClient{Client: client}, logger),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 收到中断信号后优雅关闭
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Printf("监听 %s", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("服务启动失败: %v", err)
		}
	}()

	<-stop
	logger.Println("正在关闭...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Printf("关闭超时: %v", err)
	}
	logger.Println("已退出")
}

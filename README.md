# Luogu2Api

基于 [luoguClient](https://github.com/laoin114514/luoguClient) 的洛谷 Web 服务。

## 目录结构

```
Luogu2Api/
├── go.mod                  # 服务 module（replace 指向 pkg/luoguClient）
├── main.go                 # 服务入口：flag 解析、cookie 恢复、优雅关闭
├── internal/server/        # HTTP 路由与处理器（含离线单元测试）
├── pkg/luoguClient/        # 洛谷 SDK —— git submodule（独立仓库、独立发版）
└── scripts/check.ps1       # 一次跑通两个 module 的 build/vet/test
```

## 依赖关系

服务通过 `require` + `replace` 引用仓库内的 SDK：

```go
require github.com/laoin114514/luoguClient v0.0.0
replace github.com/laoin114514/luoguClient => ./pkg/luoguClient
```

- 改 SDK 源码**立即生效**，不需要发版、不需要改版本号；
- 想改成依赖已发布版本时，删掉 `replace` 并把版本号写成 `v0.3.0` 之类即可；
- ⚠️ **`./...` 不会进入嵌套 module**：服务的 `go build ./...` / `go test ./...` 不会构建或测试 SDK，
  必须单独跑一遍（`scripts/check.ps1` 已经包含）。

## 首次克隆

```bash
git clone --recursive https://github.com/laoin114514/Luogu2Api.git

# 已经克隆过的仓库补上 submodule：
git submodule update --init

# 把 SDK 更新到最新（否则保持仓库里记录的固定提交）：
git submodule update --remote pkg/luoguClient
```

## 运行

```bash
go run . -addr :8080
go run . -addr :8080 -cookies cookies.json
```

| 接口 | 说明 |
|---|---|
| `GET /healthz` | 健康检查，返回 `{"status":"ok"}` |
| `GET /api/problem/{pid}` | 题目精简信息（示例接口，匿名可用） |

## 登录态

SDK 只在内存中保存 cookie，是否持久化由调用方决定。本服务从 `-cookies` 指定的文件读取
（默认 `cookies.json`，已在 `.gitignore` 中忽略）；文件不存在则匿名运行，需要登录的接口会返回 401。

生成 cookie 文件的示例见 SDK 的 `example/main.go`。**该文件包含会话凭据，不要提交。**

## 校验

```powershell
pwsh scripts/check.ps1
```

等价的手动命令：

```bash
go build ./... && go vet ./... && go test ./...
(cd pkg/luoguClient && go test -race ./...)
```

## 部署注意

- `git clone` 必须带 `--recursive`（CI 里对应 `submodules: true`），否则 `pkg/luoguClient` 为空、构建失败。
- 想彻底摆脱 submodule 依赖：`go mod vendor` 后提交 `vendor/`（实测可行，约 0.5 MB），构建改用 `go build -mod=vendor`。
- 用 `GOWORK=off` 可以确认构建不依赖任何本地 workspace 文件。

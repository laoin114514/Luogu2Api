module github.com/laoin114514/luogu2api

go 1.26.2

require github.com/laoin114514/luoguClient v0.0.0

require (
	github.com/PuerkitoBio/goquery v1.12.0 // indirect
	github.com/andybalholm/cascadia v1.3.3 // indirect
	golang.org/x/net v0.52.0 // indirect
)

// SDK 以 git submodule 形式放在 pkg/luoguClient，用 replace 指向本地源码：
// 改 SDK 立即生效、无需发版。若要改为依赖已发布版本，删掉 replace 并写真实版本号即可。
replace github.com/laoin114514/luoguClient => ./pkg/luoguClient

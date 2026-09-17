# fakeuseragent

Python 库 [fake-useragent](https://github.com/fake-useragent/fake-useragent)（v2.x）的 Go 移植：
内置真实世界的 User-Agent 数据，按浏览器 / 操作系统 / 设备类型过滤后随机返回一条。
**数据随包发布，运行期零网络请求。**

## 快速开始

    import "github.com/laoin114514/luogu2api/pkg/fakeuseragent"

    // 默认实例（进程级共享，懒加载）
    req.Header.Set("User-Agent", fakeuseragent.Random())

    // 固定过滤条件并复用；实例构造后只读，可并发使用
    ua, err := fakeuseragent.New(
        fakeuseragent.WithBrowsers(fakeuseragent.BrowserChrome, fakeuseragent.BrowserEdge),
        fakeuseragent.WithOS(fakeuseragent.OSWindows),
        fakeuseragent.WithPlatforms(fakeuseragent.TypeDesktop),
        fakeuseragent.WithMinVersion(120),
    )
    if err != nil {
        return err
    }
    req.Header.Set("User-Agent", ua.Random())

需要拿完整记录（版本号、系统、设备品牌等）时用 `GetBrowser` / `Lookup`：

    item, err := ua.Lookup(fakeuseragent.BrowserFirefox)
    fmt.Println(item.UserAgent, item.BrowserVersionMajorMinor, item.OS)

## API 对照

| Python | 本包 |
| --- | --- |
| `UserAgent(browsers=..., os=..., platforms=..., min_version=..., fallback=...)` | `New(WithBrowsers(...), WithOS(...), WithPlatforms(...), WithMinVersion(...), WithFallback(...))` |
| `ua.random` | `ua.Random()` / 包级 `Random()` |
| `ua.chrome`、`ua.googlechrome`、`ua.ff`、`ua.firefox`、`ua.safari`、`ua.opera`、`ua.google`、`ua.edge` | 同名方法 `Chrome()` / `GoogleChrome()` / `FF()` / `Firefox()` / `Safari()` / `Opera()` / `Google()` / `Edge()` |
| `ua["Chrome"]`、`ua.anything` | `ua.Browser("Chrome")`、`ua.Browser("anything")` |
| `ua.getRandom`、`ua.getChrome`、… | `ua.GetRandom()`、`ua.GetChrome()`、… |
| `ua.getBrowser("firefox")` | `ua.GetBrowser(BrowserFirefox)` |
| `ua.data_browsers` | `ua.Filter()`（当前过滤条件下的记录副本）、`AvailableBrowsers()`（数据里全部浏览器名） |
| — | `ua.Lookup(...)`：匹配不到时返回 `ErrNoMatch`，不静默退化 |
| `safe_attrs` | 不需要：Go 没有 `__getattr__` 那种魔法 |

Python 的 `FakeUserAgent` 别名没有再定义一份类型，用 `UserAgent` 即可。

## 选项

| 选项 | 说明 |
| --- | --- |
| `WithBrowsers(names ...string)` | 限定浏览器，**大小写敏感**，建议用 `BrowserChrome` 这类常量；不传参数沿用默认集合 |
| `WithOS(names ...string)` | 限定操作系统，同样大小写敏感 |
| `WithPlatforms(platforms ...string)` | `TypeDesktop` / `TypeMobile` / `TypeTablet` |
| `WithMinVersion(v float64)` | 主次版本号下限，0 表示不过滤 |
| `WithMinPercentage(p float64)` | 保留只为对齐 Python，新代码不建议使用（见下文） |
| `WithFallback(ua string)` | 兜底 UA，默认 `DefaultFallback` |
| `WithRand(*rand.Rand)` | 注入随机源（测试用），默认 `math/rand/v2` 全局源 |

名字写错（`WithBrowsers("chrome")` 小写、`WithOS("Ubuntu")`）会被 `New` 直接拒绝，
错误信息里列出可选值。这在 Python 版里只是"永远匹配不到 → 静默返回兜底 UA"。

## 与 Python 版的差异（都是刻意的）

1. **数据只解析一次**：Python 每构造一个实例就把整份 JSONL 重新读入内存；本包首次使用时解析一次，
   所有实例共享（`Default()` 是进程级单例）。
2. **构造期建索引**：过滤条件在 `New` 里就折算成候选下标，取 UA 是"取下标 + 随机"，
   不重复遍历一万条数据；多名字组合（如 Chrome 家族）的并集还会缓存（有上限）。
3. **非法名字 fail fast**：见上文，`New` 返回错误而不是静默兜底。
4. **默认集合去掉了死条目**：Python 默认列表里的浏览器 `Android` / `MiuiBrowser` / `Whale`、
   系统 `Ubuntu` 在数据里根本不存在，本包默认集合不再列它们（行为与 Python 等价，只是不再假装可用）。
5. **多一个严格入口**：`Lookup` 返回 `ErrNoMatch`；`Browser` / `GetBrowser` 保持 Python 的兜底语义。
6. **返回值是结构体**：`Data` 是值类型，字段与上游 JSON 一一对应（`json` tag 保留原命名），
   不再是"字段可能变的 dict"。上游 `null` 在 Go 侧是空字符串。
7. **随机是等概率的**：与 Python 一样在候选列表上均匀取样，**不按 `Percent` 加权**。

## 数据

链路（都可追溯）：

    Intoli LLC 的 user-agents 数据
      └─ fake-useragent 的 ua-converter/ua_convert.py（ua-parser 解析 + 字段重映射）
           └─ fake-useragent/src/fake_useragent/data/browsers.jsonl      ← 输入
                └─ scripts/gen_ua_data.py 逐行校验 + gzip
                     └─ pkg/fakeuseragent/data/browsers.jsonl.gz          ← 内嵌进二进制（243 KB）

当前快照（9995 行）：设备类型 `mobile` 8585 / `desktop` 1377 / `tablet` 33；
浏览器 23 种（Chrome Mobile 6169、Mobile Safari 1925、Chrome 919 排前三）；系统 6 种。

**为什么不"顺手去重"**：同一个 UA 字符串在上游会重复出现（去重后只有 5854 条，重复 4141 行），
Python 版用 `random.choice` 在"行"上等概率取样，重复行因此承担了加权作用。
去重会改变随机分布，破坏两个版本的一致性——`data_test.go` 里有一条测试专门钉住这件事。

**更新数据**：

    python scripts/gen_ua_data.py                # 从上游地址下载后重新生成
    python scripts/gen_ua_data.py --check        # 校验产物与输入是否一致（字节级）

脚本不做任何加工，只校验 + 压缩，且固定 gzip 的 mtime 与压缩级别，同样输入必得同样输出。

许可：代码与数据来自 [fake-useragent](https://github.com/fake-useragent/fake-useragent)（Apache-2.0），
其数据由 [Intoli 的 user-agents](https://github.com/intoli/user-agents) 转换而来（上游 README 声明数据使用已获 Intoli 许可）。

## 文件

    pkg/fakeuseragent/
    ├── doc.go                 包文档
    ├── data.go                Data 结构、名字常量、默认集合、内嵌数据加载
    ├── option.go              New 的构造选项
    ├── fakeuseragent.go       生成器主体、包级便捷函数、错误定义
    ├── data_test.go           数据完整性 / 重复行 / 名字覆盖
    ├── fakeuseragent_test.go  过滤、兜底、并发、缓存上限等行为
    ├── example_test.go        godoc 示例
    └── data/browsers.jsonl.gz 内嵌数据（由 scripts/gen_ua_data.py 生成）

## 验证

    go test -race ./pkg/fakeuseragent/
    python scripts/gen_ua_data.py --check

## 注意事项

- **`Percent` 不是百分比**：上游现在这个字段的取值在 0.0004 ~ 0.094 之间（全部 < 1），
  所以 `WithMinPercentage(1)` 会淘汰所有记录、结果恒为兜底 UA。这条行为与 Python 版一致
  （有测试钉住），但不要在新代码里用它做"使用率过滤"。
- 随机倾斜：数据里移动端占绝大多数，所以 `Random()` 大概率返回手机 UA；
  要桌面 UA 请显式 `WithPlatforms(TypeDesktop)`。
- 数据是随包发布的快照，新鲜度取决于重新执行生成脚本的时机，不会自己更新。
- 内嵌数据（含解析后的记录）常驻内存，几 MB 量级；这是"零网络请求"的代价。
- 想把 UA 用到洛谷请求上：`sdk.NewClient(sdk.WithUserAgent(fakeuseragent.Random()))` 即可
  （注意 SDK 的 UA 在构造期固定，一个 client 一个 UA，做不到"每请求换一个"）。

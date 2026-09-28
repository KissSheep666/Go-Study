# GoStudy — Java 后端开发者的 Go 上手计划

## 背景与目标

- **学习者画像**：有 Java 后端经验，熟悉 Spring Boot 那一套。
- **目标方向**：Go 后端 Web 服务/API + 并发/中间件/基础设施。
- **学习方式**：先用 Java 类比对照铺前置知识，再通过一个经典小项目把知识串起来。
- **四大覆盖域**：
  1. 语言核心与思维转变
  2. 并发模型（Go 的看家本领）
  3. 工程化与工具链
  4. 后端生态实操

## 目录结构

```
GoStudy/
├── README.md                       # 本文件：路线图与进度
├── notes/
│   ├── 01-java-to-go.md            # 前置知识：Java → Go 全对照
│   └── 02-m1-shortlink.md          # M1 完整源码教程（含全部源码 + 逐块讲解）
└── shortlink/                      # 练手项目
    ├── go.mod
    ├── main.go                     # 入口 + 路由
    ├── link.go                     # Link 模型 + 短码生成 + URL 校验
    ├── store.go                    # 内存存储（map + RWMutex）
    ├── handler.go                  # HTTP 处理函数
    └── handler_test.go             # 表驱动测试 + 并发测试
```

## 前置知识

已完成：[`notes/01-java-to-go.md`](notes/01-java-to-go.md)

涵盖：七条思维转变、语法速查表、并发模型对照、工程化对照、标准库对照、Web 最小对照、Top 12 踩坑清单。

---

## 练手项目：短链接服务 `shortlink`

### 为什么是它

一个足够小、又能一路长出来的项目，每个阶段都在**加东西而不是推倒重来**：

- 有 CRUD → 练分层、接口、依赖注入
- 有跳转 → 练 HTTP 语义、中间件
- 有短码生成和计数 → 练并发（两种风格对比）
- 有缓存 → 练 context 超时传播、缓存击穿
- 要上线 → 练工程化、测试、Docker

### 阶段进度

| 阶段 | 主题 | 状态 | 产出 |
|---|---|---|---|
| **M0** | 环境搭建 | ✅ 完成 | Go 1.27.1 + mingw-w64 + VS Code + `-race` 可用 |
| **M1** | 纯 `net/http` + 内存存储 | ✅ 完成 | [教程](notes/02-m1-shortlink.md) · 5 个接口全通 · `-race` 无警告 |
| **M2** | 分层 + 接口 + 构造注入 | ⬜ 待开始 | |
| **M3** | 中间件（日志/recover/请求ID/限流/CORS） | ⬜ 待开始 | |
| **M4** | 并发（Base62 发号器、计数聚合、worker pool） | ⬜ 待开始 | |
| **M5** | 存储（SQLite/Postgres + Redis、singleflight） | ⬜ 待开始 | |
| **M6** | 工程化（表驱动测试、pprof、优雅关闭、slog、Docker） | ⬜ 待开始 | |
| **M7** | 用 Gin 重写对比 | ⬜ 待开始 | |

### M1 成果

```
$ go build -o shortlink.exe .          # 8.3 MB 单文件
$ go test -v .                         # ok  shortlink  0.514s
$ CGO_ENABLED=1 go test -race .        # ok  shortlink  1.591s  （无 DATA RACE）

GET  /health         → 200 {"status":"ok","links":0}
POST /api/links      → 201 {"code":"aB3xK9","url":"...","hits":0}
GET  /{code}         → 302 Location: <原始长链接>
GET  /nonexistent    → 404 {"error":"link not found"}
POST 非法 URL         → 400 {"error":"url must start with http:// or https://"}
```

**M1 刻意保持扁平单包**——不分层、无依赖注入。这是为 M2 铺路：先感受痛，再学解法。

---

## 环境备忘

| 组件 | 位置 |
|---|---|
| Go SDK | `D:\Dev\go`（1.27.1，GOROOT 自动推断） |
| GOPATH | `D:\Dev\gopath` |
| mingw-w64 GCC | `D:\Dev\mingw64`（16.2.0，`-race` 需要） |
| VS Code 扩展 | `golang.go` |
| Go 工具 | gopls / dlv / goimports / staticcheck |

### 常用命令

```bash
# 环境
go version
go env GOPROXY GOPATH GOROOT

# 开发
gofmt -l -w .                          # 格式化
go vet .                               # 静态检查
go build -o shortlink.exe .            # 编译
go run .                               # 直接运行

# 测试（Windows 上 -race 需要 gcc 在 PATH 里）
go test -v .
CGO_ENABLED=1 go test -race -v .
```

### 环境注意事项

1. **端口**：8080 / 8081 / 8000 / 5173 已被本机其他项目占用，shortlink 用 **8090**
2. **内存**：本机 15.7 GB，可用常年不足 1 GB。写 Go 时建议关掉 Docker Desktop 和浏览器——`go vet ./...`、`-race`、gopls 都很吃内存
3. **`CGO_ENABLED` 分工**：全局保持 `0`（产出纯 Go 静态单文件），只在跑 `-race` 时设 `1`
4. **GitHub**：`api.github.com` 直连可用，但 `objects.githubusercontent.com`（release 附件）被墙，需走 FlClash 代理 `127.0.0.1:7890`

---

## 参考

- [A Tour of Go](https://go.dev/tour/) — 官方交互式教程
- [Effective Go](https://go.dev/doc/effective_go) — 官方风格指南
- [Go by Example](https://gobyexample.com/) — 例子驱动
- [50 Shades of Go](https://golang50shades.com/) — 常见陷阱
- [Go 内存模型](https://go.dev/ref/mem) — 理解 `-race` 的理论基础

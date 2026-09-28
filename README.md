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
├── README.md                 # 本文件：路线图与项目规划
├── notes/
│   └── 01-java-to-go.md      # 前置知识：Java → Go 全对照
└── shortlink/                # 练手项目（M1 开始创建）
```

## 前置知识

已完成：[`notes/01-java-to-go.md`](notes/01-java-to-go.md)

涵盖：七条思维转变、语法速查表、并发模型对照、工程化对照、标准库对照、Web 最小对照、Top 12 踩坑清单。

## 练手项目：短链接服务 `shortlink`

### 为什么是它

一个足够小、又能一路长出来的项目，每个阶段都在**加东西而不是推倒重来**，能自然覆盖全部四个学习域：

- 有 CRUD → 练分层、接口、依赖注入
- 有跳转 → 练 HTTP 语义、中间件
- 有短码生成和计数 → 练并发（两种风格对比）
- 有缓存 → 练 context 超时传播、缓存击穿
- 要上线 → 练工程化、测试、Docker

### 阶段规划

| 阶段 | 主题 | 做什么 | 覆盖知识点 |
|---|---|---|---|
| **M0** | 环境 | 装 Go + IDE，配 GOPROXY | `go env`、工具链 |
| **M1** | 裸写 | 纯 `net/http` + 内存 map 实现创建/跳转 | struct、方法、隐式接口、`http.Handler`、error、defer、JSON |
| **M2** | 分层 | 拆 `handler`/`service`/`repository`，接口解耦，手写构造注入 | 接口定义在使用方、`internal/`、包组织、命名规范 |
| **M3** | 中间件 | 日志、panic recover、请求 ID、令牌桶限流、CORS | 函数装饰器、闭包、`context` 传值、对标 Spring Filter |
| **M4** | 并发 | Base62 发号器、访问计数聚合、批量预热 worker pool | goroutine、channel、select、atomic、WaitGroup、errgroup、两种并发风格对比 |
| **M5** | 存储 | 接 SQLite/Postgres + Redis 缓存 | `database/sql`、连接池、context 超时传播、singleflight 防击穿 |
| **M6** | 工程化 | 表驱动测试、`-race`、benchmark、pprof、优雅关闭、slog、Docker | 测试、性能分析、`signal.NotifyContext`、多阶段构建 |
| **M7** | 换框架 | 用 Gin 重写一遍，对比手写 net/http 的取舍 | 框架选型、生态实操 |

### 每一阶段都产出一个可运行版本

`v1` → `v2` … 逐步演进，每一步都有可跑的二进制和测试。

## 环境备忘

国内必配（装完 Go 后执行）：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
go env -w GOSUMDB=sum.golang.google.cn
```

验证：

```bash
go version
go env GOPROXY
```

## 备注

用户在 M0 阶段自行安装 Go 环境。

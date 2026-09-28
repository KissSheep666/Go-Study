# M1 完整源码教程：短链接服务

> **本文档包含 M1 的全部源码 + 逐块讲解。**
> 建议用法：先通读第 1、2 节理解整体，然后照着文档**把代码一个字一个字敲进编辑器**。
> 敲的过程中卡住了，再回来看对应文件的「重点讲解」。
> 全部敲完后跑第 9 节的验证命令，全绿就说明 M1 过关。
>
> 前置阅读：[`01-java-to-go.md`](01-java-to-go.md)

## 目录

- [1. M1 目标与验收标准](#1-m1-目标与验收标准)
- [2. 环境与项目初始化](#2-环境与项目初始化)
- [3. 文件 1/6：`go.mod`](#3-文件-16gomod)
- [4. 文件 2/6：`link.go` —— 数据模型与短码生成](#4-文件-26linkgo--数据模型与短码生成)
- [5. 文件 3/6：`store.go` —— 并发安全的内存存储](#5-文件-36storego--并发安全的内存存储)
- [6. 文件 4/6：`handler.go` —— HTTP 处理函数](#6-文件-46handlergo--http-处理函数)
- [7. 文件 5/6：`main.go` —— 组装与启动](#7-文件-56maingo--组装与启动)
- [8. 文件 6/6：`handler_test.go` —— 测试](#8-文件-66handler_testgo--测试)
- [9. 跑起来验证](#9-跑起来验证)
- [10. Java ↔ Go 对照总表](#10-java--go-对照总表)
- [11. 七个关键知识点](#11-七个关键知识点)
- [12. 踩坑记录](#12-踩坑记录)
- [13. 自测题（含答案）](#13-自测题含答案)
- [14. M2 预告](#14-m2-预告)

---

## 1. M1 目标与验收标准

**一句话**：用**标准库**写出一个能跑的短链接服务，**不引任何第三方依赖**。

M1 刻意**不分层、不做依赖注入**——全部代码扁平地放在一个 `package main` 里。

> ⚠️ **这是故意的。** 到 M2 你会真切感受到扁平代码的痛（全局变量、测试互相污染、无法替换实现），然后才明白分层和依赖注入为什么存在。
> **先让你痛，再给你药。**

### 验收标准

| # | 标准 | 接口 |
|---|---|---|
| 1 | 创建短链 | `POST /api/links` → `201` + `{"code":"aB3xK9",...}` |
| 2 | 跳转 | `GET /{code}` → `302` + `Location` 头 |
| 3 | 健康检查 | `GET /health` → `200` + 链接总数 |
| 4 | 非法 URL / 非法 JSON | `400` |
| 5 | 不存在的短码 | `404` |
| 6 | 访问计数累加 | 每次跳转 `hits + 1` |
| 7 | **并发安全** | `-race` 无警告 |
| 8 | 有测试覆盖 | `go test` 全绿 |

### 文件清单

```
shortlink/
├── go.mod              3 行    模块定义
├── link.go            93 行    数据模型 + 短码生成 + URL 校验
├── store.go          116 行    内存存储（map + RWMutex）
├── handler.go        172 行    HTTP 处理函数 + 响应辅助
├── main.go           134 行    入口 + 路由 + 启动
└── handler_test.go   339 行    表驱动测试 + 并发测试
```

---

## 2. 环境与项目初始化

### 2.1 确认环境

```bash
go version
# go version go1.27.1 windows/amd64   ← 需要 1.22+（用到新路由语法）
```

> **为什么必须 1.22+**：本文档里 `mux.HandleFunc("GET /health", ...)` 这种"方法写在路径里"的语法、以及 `r.PathValue("code")` 都是 Go 1.22 才有的。1.22 之前要引第三方路由库。

如果用 VS Code，先确认右下角状态栏显示 `go1.27.1`（不显示就完全退出 VS Code 再打开）。

### 2.2 建项目

```bash
mkdir shortlink && cd shortlink
go mod init shortlink
```

`go mod init` ≈ Maven 的"创建 pom.xml"。

> **`go mod init` 后面跟什么？**
> 这里写的是短名字 `shortlink`。正式项目应该写成最终的 import 路径，比如 `github.com/KissSheep666/shortlink`——因为别人 import 你的包时用的就是这个字符串。
> 本地练手用短名字完全没问题（M1 只有一个包，压根不会被 import）。

执行完会生成 `go.mod`，下面开始逐个文件写。

---

## 3. 文件 1/6：`go.mod`

`go mod init shortlink` 自动生成的，**不用手敲**：

```
module shortlink

go 1.27.1
```

就这三行。对照 Maven：

| Maven | Go | 说明 |
|---|---|---|
| `pom.xml` | `go.mod` | |
| `groupId:artifactId:version` | `module shortlink` | 用最终 import 路径 |
| `<maven.compiler.source>` | `go 1.27.1` | 声明语言版本 |
| 父子 pom、`<dependencyManagement>` | **不存在** | Go 依赖是**扁平**的，全在一个 `require` 块里 |
| 无对应物 | `go.sum` | 依赖校验和，**必须提交**（≈ `package-lock.json`） |

**M1 零第三方依赖**，所以没有 `require` 块，也**不会生成 `go.sum`**（M5 引入数据库驱动时才出现）。

---

## 4. 文件 2/6：`link.go` —— 数据模型与短码生成

这个文件负责三件事：定义 `Link` 长什么样、生成短码、校验 URL。

### 完整源码

```go
package main

import (
	"errors"
	"math/rand/v2"
	"net/url"
	"time"
)

// Link 是一条短链接记录。
//
// Java 对照：一个 POJO / JPA Entity。
// 差别在于：
//   - 没有 getter/setter。字段直接导出（首字母大写），谁都能读写
//   - 没有 Lombok。你以为需要 @Data，其实 Go 的 struct 自带这些
//   - 反引号里的是 struct tag，供标准库的反射读取。
//     它【不是注解】—— 注解是给框架在运行时扫描的，
//     tag 只是一段附在字段上的字符串，谁来解析、怎么解析，全看调用方
type Link struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
	Hits      int64     `json:"hits"`
}

const (
	// CodeLen 是短码长度。62^6 ≈ 568 亿种组合，够用了。
	CodeLen = 6

	// CodeAlphabet 是短码字符集：a-z A-Z 0-9 共 62 个。
	// 这就叫 Base62 编码 —— M4 会用它把自增 ID 转成短码。
	CodeAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// ErrInvalidURL 是 URL 校验失败的哨兵错误。
//
// Java 对照：自定义 Exception 类。
// Go 里错误是【值】不是类型，所以惯例是定义成包级的 var。
//   - 命名以 Err 开头
//   - 用 errors.New 创建
//   - 调用方用 errors.Is(err, ErrInvalidURL) 判断，
//     而不是 Java 那样 catch 一个异常类型
var ErrInvalidURL = errors.New("url must start with http:// or https://")

// generateCode 随机生成一个短码。
//
// 注意用的是 math/rand/v2（Go 1.22+），不是老的 math/rand：
//   - v2 的全局函数【自动随机播种】，不再需要 rand.Seed(time.Now().UnixNano())
//     老代码里那句 Seed 现在已经是废弃写法了
//   - v2 的顶层函数【并发安全】，多个 goroutine 同时调用没问题
//
// Java 对照：SecureRandom / ThreadLocalRandom
//
// 这个实现的问题（M4 会改）：随机生成有【碰撞可能】——
// 两张不同的长链接可能抽到同一个短码，那时候 Save 会直接覆盖掉前一条。
// M4 会换成"自增 ID + Base62 编码"，从根上杜绝碰撞。
func generateCode() string {
	// make([]byte, CodeLen) 创建一个长度为 6 的字节切片。
	// Java 对照：new byte[6]
	b := make([]byte, CodeLen)
	for i := range b {
		// rand.IntN(n) 返回 [0, n) 的随机整数。
		// 用它当下标去字符集里取一个字符。
		b[i] = CodeAlphabet[rand.IntN(len(CodeAlphabet))]
	}
	// string(b) 把 []byte 转成 string。
	// 注意：Go 里 []byte 和 string 的互转【会拷贝数据】，
	// 所以别在热路径里反复转。
	return string(b)
}

// validateURL 校验长链接是否可用。
//
// 这是 Go 最典型的错误处理形态：返回 error，让调用方决定怎么办。
// Java 对照：一个会 throw IllegalArgumentException 的校验方法。
// 区别是 Java 里你【可以】选择不 catch（运行时异常），
// Go 里你必须显式处理返回值，否则编译器不拦你、但 linter 和同事会。
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidURL
	}
	// 只接受 http / https，挡掉 javascript: 这类 XSS 载体
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrInvalidURL
	}
	// 必须有 host，挡掉 "https://" 这种半截货
	if u.Host == "" {
		return ErrInvalidURL
	}
	return nil
}
```

### 重点讲解

#### ① 写文件的第一行永远是 `package`

```go
package main
```

**同一目录下所有 `.go` 文件必须属于同一个包**。所以你敲 `link.go` / `store.go` / `handler.go` / `main.go` 时，第一行都是 `package main`。

| | Java | Go |
|---|---|---|
| 声明 | `package com.example.shortlink;` | `package main` |
| 命名 | 域名倒写 + 分层 | **一个短小写单词** |
| 目录关系 | 包名必须匹配目录结构 | 包名和目录名**可以不同**（但惯例保持一致） |
| 特殊包 | 无 | **`main` 编译成可执行文件**，其他包编译成库 |

#### ② struct 没有 getter/setter

```go
type Link struct {
	Code string // 大写 = 导出（包外可见）≈ public
	url  string // 小写 = 包内私有 ≈ package-private
}
```

Java 开发者第一反应是"这破坏封装"。但 Go 的哲学是：

> **封装靠包（package），而不是靠类。**

同一个包内互相信任；跨包才需要边界。而且 Go 有 `internal/` 目录做**编译期强制**的模块边界（M2 用到），颗粒度比 `private` 粗，但更难绕过。

没有 `protected` 这种东西——Go 里不存在"子类可见"的概念，因为**没有继承**。

#### ③ struct tag 不是注解

```go
Code string `json:"code"`
```

反引号里的是 **struct tag**，本质就是**附在字段上的一段字符串**。`encoding/json` 包通过反射读取它，按自己的规则解析。

| | Java 注解 | Go struct tag |
|---|---|---|
| 本质 | 类型系统的一部分 | **纯字符串** |
| 谁处理 | 框架在运行时扫描 / 编译期处理器 | 谁想读谁读，标准库自己解析 |
| 语法检查 | 编译器检查 | **不检查**，写错了运行时才发现 |
| 例子 | `@JsonProperty("code")` | `` `json:"code"` `` |

> **踩坑预警**：tag 写错了编译器不管。比如写成 `` `json:"code"` `` 少个引号、或者 `` `json:code` ``，编译照样过，只是序列化出来的字段名不对。改完 tag 一定要 curl 验证一下。

#### ④ `int64` 而不是 `int`

```go
Hits int64
```

Go 的 `int` 是**平台相关**的（64 位机器上是 64 位，32 位机器上是 32 位）。涉及序列化、跨平台传输、数据库映射时，**一律用明确位数的类型**：`int64` / `int32`。

Java 对照：`int` 永远是 32 位、`long` 永远是 64 位，没这个问题。所以这是 Go 独有的坑。

**惯例**：
- 计数、ID、时间戳 → `int64`
- 循环下标、长度 → `int`

#### ⑤ 常量块与 `iota`

```go
const (
	CodeLen      = 6
	CodeAlphabet = "..."
)
```

`const` 块就是多个常量写一起，**没有 `iota` 时它只是语法糖**（等价于写两行 `const`）。

> 顺带说下 `iota`（本文件没用到，但你会经常见到）：
> ```go
> type Status int
> const (
>     StatusPending Status = iota // 0
>     StatusRunning               // 1
>     StatusDone                  // 2
> )
> ```
> Java 对照：`enum`。区别是 Go 的 `iota` 枚举**没有类型安全保证**——你可以把任意 `int` 转成 `Status`，编译器不拦。

**Go 的常量是无类型的**，这点比 Java 灵活：

```go
const N = 6          // 无类型常量
var a int = N        // OK
var b int64 = N      // OK，同一个常量能赋给不同数值类型
var c float64 = N    // OK，也行
```

Java 里 `static final int N = 6` 是 `int` 类型，赋给 `long` 要隐式转换。Go 的无类型常量能自动适配。

#### ⑥ 哨兵错误（Sentinel Error）

```go
var ErrInvalidURL = errors.New("url must start with http:// or https://")
```

| | Java | Go |
|---|---|---|
| 定义 | `class InvalidUrlException extends RuntimeException` | `var ErrInvalidURL = errors.New("...")` |
| 抛出 | `throw new InvalidUrlException()` | `return ErrInvalidURL` |
| 判断 | `catch (InvalidUrlException e)` | `errors.Is(err, ErrInvalidURL)` |
| 本质 | **类型** | **值** |

**命名惯例**：
- 错误变量以 `Err` 开头：`ErrInvalidURL`、`ErrNotFound`、`ErrDuplicate`
- 错误信息**全小写、不加标点、不重复上下文**：`"url must start with http://"` 而不是 `"URL must start with HTTP://."`

第三条尤其重要：因为错误会被层层包装，最终打印出来是 `create link: validate: url must start with http://`。如果每条错误信息首字母都大写、都带句号，拼起来就是灾难。

#### ⑦ `math/rand/v2`（Go 1.22+）

```go
import "math/rand/v2"
b[i] = CodeAlphabet[rand.IntN(len(CodeAlphabet))]
```

和老的 `math/rand` 的区别：

| | `math/rand`（旧） | `math/rand/v2`（新） |
|---|---|---|
| 播种 | 必须 `rand.Seed(time.Now().UnixNano())`，忘了就每次跑出同一串 | **自动**，无需任何操作 |
| 并发安全 | 顶层函数安全但性能一般 | 顶层函数**并发安全**且更快 |
| 算法 | 可配置 | ChaCha8（默认） |
| Java 对照 | `new Random(seed)` | `ThreadLocalRandom` |

> **不要再用 `rand.Seed`** —— Go 1.20 起它已经废弃，1.22 的 v2 里干脆没有了。

#### ⑧ 短码生成的设计缺陷（M4 要修）

```go
b[i] = CodeAlphabet[rand.IntN(len(CodeAlphabet))]
```

**这是随机生成，有碰撞可能。**

62^6 ≈ 568 亿种组合。听起来很多，但根据**生日悖论**，大概生成到 1000 万条左右时，碰撞概率就过半了。真实流量下这是必然事件，而碰撞的后果是——`Store.Save` 会**静默覆盖**前一条链接。用户拿到一个短码，跳转到的却是别人的链接。

M4 会换成 **自增 ID + Base62 编码**：
- 用 `atomic.Int64` 生成单调递增的 ID
- 把 ID 当成 62 进制的数，逐位映射到 `CodeAlphabet`
- ID 不重复 → 短码**从数学上不可能碰撞**

顺便练原子操作和进制转换。

#### ⑨ URL 校验为什么必须有

```go
if u.Scheme != "http" && u.Scheme != "https" {
	return ErrInvalidURL
}
```

如果不校验 scheme，用户能提交 `javascript:alert(document.cookie)`。我们的服务会返回 `302 Location: javascript:...`，浏览器**会执行它**——典型的**开放重定向 + XSS**。

`u.Host == ""` 那个检查挡的是 `"https://"` 这种只有 scheme 没有主机的半截货。

> 测试用例里的 `{"危险 scheme", "javascript:alert(1)", true}` 就是在验证这个。

---

## 5. 文件 3/6：`store.go` —— 并发安全的内存存储

**这是 M1 最重要的文件。** 短链接的所有数据都存在这里，而 HTTP 服务器是并发的——多个请求同时读写同一个 map，稍有不慎就是数据竞争。

### 完整源码

```go
package main

import "sync"

// Store 是短链接的内存存储。
//
// Java 对照：直接翻译的话你会写 ConcurrentHashMap<String, Link>。
// 但这里【故意】用普通 map + RWMutex —— 原因见下面 mu 的注释。
type Store struct {
	// mu 保护下面的 links。
	//
	// 【关键认知】Go 的 map 不是并发安全的，而且比 Java 严重得多：
	//   - Java 的 HashMap 并发写：数据错乱、可能死循环，但不一定崩
	//   - Go 的 map 并发写：直接 fatal error: concurrent map writes，
	//     整个进程【立即挂掉】，而且这是 runtime 抛的，recover 都救不回来
	//
	// 所以只要有多个 goroutine 碰同一个 map，就必须加锁。
	//
	// RWMutex vs Mutex：读写锁，允许多个读者并发。
	// Java 对照：ReentrantReadWriteLock。
	//
	// 另外注意 mu 这个字段本身：sync.RWMutex 的【零值就可以直接用】，
	// 不需要显式初始化。这就是 Go 的"零值可用"设计原则。
	mu sync.RWMutex

	// links 存【值】而不是 *Link —— 这是刻意选的，不是随手的。
	//
	// 如果存 map[string]*Link：
	//   → Get 返回指针，调用方就在【没有锁保护】的情况下持有共享可变状态
	//   → 它读 l.Hits 的同时，另一个 goroutine 在 IncrementHits 里写 l.Hits
	//   → 这是真正的数据竞争，-race 会抓出来
	//   → 而且你自己很难发现，因为锁看起来"加了"
	//
	// 存值的话，Get 返回的是【拷贝】，任何人拿到的都是自己的一份，
	// 共享可变状态从根上消失了。
	//
	// 这背后是 Go 的核心哲学：
	//   能不共享就不共享；非要共享，就让锁的范围和数据生命周期对齐。
	links map[string]Link
}

// NewStore 创建一个空的 Store。
//
// Go 没有构造函数语法，惯例是写一个 NewXxx 函数返回指针。
// Java 对照：一个构造器 + 静态工厂方法。
func NewStore() *Store {
	return &Store{
		// map 必须 make 才能写。
		// nil map 可以读（返回零值），但写入会 panic —— 这是新手必踩的坑。
		links: make(map[string]Link),
	}
}

// Save 保存一条链接。
//
// 接收 Link 值而不是指针：调用方传进来的 struct 会被【拷贝】一份存起来，
// 之后调用方再改自己那份也不会影响存储。这就是值语义带来的隔离。
func (s *Store) Save(l Link) {
	s.mu.Lock()
	// defer 在这里是惯用法：加锁后立刻写 defer Unlock，
	// 无论函数从哪个分支返回、甚至中途 panic，锁都会被释放。
	// Java 对照：try-with-resources 或者 try/finally。
	defer s.mu.Unlock()

	s.links[l.Code] = l
}

// Get 按短码取链接，第二个返回值表示"是否存在"。
//
// 这里的 (Link, bool) 双返回值是 Go 的招牌写法，叫 "comma ok"。
// Java 对照：你会返回 Optional<Link> 或者 null。
//   - 返回 nil 不行 —— Link 是值类型，没有 nil
//   - 返回 *Link 又要处理 nil 指针
//   - 所以 Go 选择多返回一个 bool
//
// 调用方这样写：
//
//	link, ok := store.Get("abc123")
//	if !ok { /* 不存在 */ }
func (s *Store) Get(code string) (Link, bool) {
	// RLock 是读锁，多个 goroutine 可以同时持有。
	s.mu.RLock()
	defer s.mu.RUnlock()

	l, ok := s.links[code]
	// 返回拷贝。调用方拿到后随便改，都影响不到存储里的那份。
	return l, ok
}

// IncrementHits 给某个短码的访问计数 +1。
func (s *Store) IncrementHits(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.links[code]
	if !ok {
		return
	}
	l.Hits++
	// 【必须写回】—— 这行是最容易漏的。
	//
	// 因为 l 是从 map 里【拷贝】出来的一个副本，改 l.Hits 只改了副本。
	// 不写回的话，计数永远是 0。
	//
	// Java 里 map.get() 拿到的是对象引用，改它就直接改到 map 里的对象了，
	// 所以 Java 开发者 100% 会漏这一行。
	s.links[code] = l
}

// Len 返回当前存储的链接数量。
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.links)
}
```

### 重点讲解

#### ① Go 的 map 并发写会直接杀死进程

| | Java `HashMap` | Go `map` |
|---|---|---|
| 并发写后果 | 数据错乱、可能死循环 | **`fatal error: concurrent map writes`** |
| 进程状态 | 通常还活着 | **立即终止** |
| 能否 recover | — | **不能**，这是 runtime 直接抛的 |

Go 的 runtime 内置了并发写检测（map 的 header 里有个 `flags` 位），一旦发现两个 goroutine 同时写，直接终止整个进程。

**所以：只要有多个 goroutine 碰同一个 map，就必须加锁。** 没有例外。

#### ② 为什么不用 `sync.Map`（≈ `ConcurrentHashMap`）

这是 Go 和 Java 直觉**最冲突**的地方。Java 里 `ConcurrentHashMap` 几乎是默认选择；但 Go 官方文档明确说 `sync.Map` 只适合两种场景：

1. key 基本不变（写一次读多次）
2. 多个 goroutine 读写**互不相交**的 key 集合

**其他情况用普通 `map` + `Mutex` 更快也更清晰。**

原因：`sync.Map` 为了通用性做了大量 `interface{}` 装箱和原子操作，且它的读优化在键集合频繁变化时会退化。而 `map + RWMutex` 的锁竞争在大多数场景下根本不是瓶颈。

> **Java 对照记忆法**：把 `sync.Map` 当成 Java 里的 `Collections.synchronizedMap` 那种"特殊场景工具"，而不是默认选项。

#### ③ 【核心】为什么存值而不存指针

这是整个 M1 最重要的设计决策，值得单独展开。

**如果写成 `map[string]*Link`**，代码看起来完全正常：

```go
func (s *Store) Get(code string) (*Link, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.links[code]
	return l, ok          // 返回指针
}
```

调用方：

```go
link, ok := store.Get(code)                        // ① RLock/RUnlock 在这行结束
http.Redirect(w, r, link.URL, http.StatusFound)    // ② 读 link.URL —— 【锁外】
store.IncrementHits(code)                          // ③ 另一个请求在这写 link.Hits
```

**问题**：`Get` 里确实加了锁，但**锁在返回指针的那一刻就失效了**。调用方拿着指针在锁外读，另一个 goroutine 在锁内写——这是**货真价实的数据竞争**。

而你看代码时，`Get` 里明明有 `RLock`，很容易觉得"锁加了，没问题"。

**存值的话**：

```go
func (s *Store) Get(code string) (Link, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.links[code]
	return l, ok          // 返回【拷贝】
}
```

调用方拿到的是自己的一份拷贝，怎么读都不影响别人，**共享可变状态从根上消失了**。

> **背后的 Go 哲学**：
> 1. 「不要通过共享内存来通信，而要通过通信来共享内存」
> 2. 「能不共享就不共享；非要共享，就让锁的范围和数据的生命周期对齐」

**这也是为什么 M1 必须跑 `-race`** —— 这类 bug 靠肉眼看代码基本看不出来。

#### ④ `IncrementHits` 里最容易漏的一行

```go
l, ok := s.links[code]   // l 是【拷贝】，和 map 里的那份是两个独立的值
l.Hits++                 // 只改了副本
s.links[code] = l        // ← 不写回，计数永远是 0
```

**Java 开发者 100% 会漏这一行。** 因为 Java 里：

```java
Link l = map.get(code);   // 拿到的是【对象引用】
l.hits++;                 // 直接改到了 map 里的那个对象
```

Go 的赋值是**值拷贝**（详见 `01-java-to-go.md` 第 5 条），所以必须显式写回。

> 测试 `TestCreateThenRedirect` 的最后一段专门断言这个：
> ```go
> if link.Hits != 1 {
>     t.Errorf("Hits = %d, 期望 1（IncrementHits 里漏写 s.links[code] = l？）", link.Hits)
> }
> ```

#### ⑤ `sync.RWMutex` 零值可用

```go
var mu sync.RWMutex   // 直接就能用
```

不需要 `new`，不需要构造函数，不需要初始化。这就是 Go 的**零值可用（zero value is useful）**原则。

标准库里满足这个原则的类型：`sync.Mutex`、`sync.RWMutex`、`sync.WaitGroup`、`bytes.Buffer`、`strings.Builder`。

> **反例**：`map` 是**不满足**的——`nil map` 能读不能写。所以 `NewStore` 里必须 `make`。
> 你自己设计类型时也应该追求零值可用，这是 Go 代码质量的重要标志。

#### ⑥ `defer` 加锁的惯用法

```go
s.mu.Lock()
defer s.mu.Unlock()
```

**加锁后立刻写 `defer Unlock`，中间不要插任何代码。**

好处：无论函数从哪个分支返回、甚至中途 panic，锁都会被释放。Java 对照：`try-finally` 或 `try-with-resources`。

> **反面教材**：在 `Lock` 和 `defer Unlock` 之间写业务逻辑，然后中间某个分支 `return` 了——如果你忘了 `defer` 直接写 `Unlock`，就死锁了。

#### ⑦ 「comma ok」双返回值

```go
link, ok := store.Get("abc123")
if !ok {
	// 不存在
}
```

| | Java | Go |
|---|---|---|
| 找不到返回什么 | `null` / `Optional.empty()` | 第二个返回值 `false` |
| 调用方 | `if (x != null)` / `x.isPresent()` | `if !ok` |
| 为什么会这样 | — | `Link` 是**值类型**，没有 `nil`；返回 `*Link` 又要处理 nil 指针 |

Go 里到处都是这个模式：

```go
v, ok := m[key]           // map 取值
v, ok := <-ch             // channel 接收（ok=false 表示 channel 已关闭）
n, err := f()             // 函数返回错误
```

#### ⑧ 为什么 `Save` 接收值而不是指针

```go
func (s *Store) Save(l Link) {     // 值，不是 *Link
	s.links[l.Code] = l
}
```

调用方 `store.Save(link)` 时，`link` 会被**整个拷贝**一份存进 map。之后调用方再改自己那份 `link`，**完全影响不到存储里的数据**。

这就是值语义带来的天然隔离——不需要防御性拷贝（defensive copy），不需要 `clone()`。

> Java 里你必须小心：`map.put(code, link)` 之后别人改了 `link`，map 里的也跟着变。所以 Java 常常要做 defensive copy。Go 的值语义免费给了你这个。

---

## 6. 文件 4/6：`handler.go` —— HTTP 处理函数

这一层负责把 HTTP 请求翻译成对 `Store` 的调用，再把结果写回响应。

### 完整源码

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// createLinkRequest 是 POST /api/links 的请求体。
//
// 小写开头 = 包内私有。它只是内部用来接 JSON 的临时结构，
// 没必要对外暴露，所以不导出。
//
// Java 对照：一个 @RequestBody 的 DTO。
// 注意 Go 不需要单独的 DTO 类文件 —— 用到的地方就地定义就行，
// 匿名 struct 也可以（见下面的用法）。
type createLinkRequest struct {
	URL string `json:"url"`
}

// handleCreateLink 处理 POST /api/links。
//
// Java 对照：
//
//	@PostMapping("/api/links")
//	public ResponseEntity<Link> create(@RequestBody CreateLinkRequest req) {
//	    ...
//	}
//
// 三个签名上的差异，值得记住：
//
//  1. 没有返回值。响应通过 w 写出去 —— 这是"副作用式"的写法，
//     不像 Spring 那样 return 一个对象让框架去序列化
//  2. 参数是 (w, r)，w 在前。这是 http.Handler 接口规定的顺序
//  3. 函数名小写 —— 它不是给外部调用的 API，只是包内的一个处理函数
func handleCreateLink(w http.ResponseWriter, r *http.Request) {
	// 声明变量再 Decode，而不是用 := 一步到位。
	// gofmt 风格里 var 声明单独一行，下面再用 &req 取地址传进去。
	var req createLinkRequest

	// json.NewDecoder(r.Body).Decode(&req) 把请求体 JSON 解析进 req。
	//   - &req 取地址，因为 Decode 要往这个变量里写，必须传指针
	//   - Java 对照：objectMapper.readValue(body, CreateLinkRequest.class)
	//
	// 【重要】Go 的 JSON 解码默认【忽略未知字段】，
	// 而且不会像 Jackson 那样因为字段缺失就报错 —— 缺失的字段保持零值。
	// 想要严格模式得用 DisallowUnknownFields()。
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// 校验。validateURL 返回的 err 可能是 ErrInvalidURL。
	if err := validateURL(req.URL); err != nil {
		// 这里把 err.Error() 直接透出去 —— 因为 validateURL 是内部函数，
		// 错误信息是我们自己控制的，不涉及信息泄露。
		// 真实项目里要小心：不要把数据库错误、文件路径这类内部信息返回给客户端。
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	link := Link{
		Code:      generateCode(),
		URL:       req.URL,
		CreatedAt: time.Now(),
		Hits:      0,
	}

	// 直接用包级的 store 全局变量 —— M1 故意这么写，M2 会改掉。
	// 问题见 main.go 里 store 的注释。
	store.Save(link)

	writeJSON(w, http.StatusCreated, link)
}

// handleRedirect 处理 GET /{code}，把短码还原成 302 跳转。
//
// Java 对照：
//
//	@GetMapping("/{code}")
//	public ResponseEntity<Void> redirect(@PathVariable String code) {
//	    return ResponseEntity.status(302).location(URI.create(url)).build();
//	}
func handleRedirect(w http.ResponseWriter, r *http.Request) {
	// 【Go 1.22 的新能力】从路径里取参数。
	//
	// 路由注册时写的是 "GET /{code}"，这里的 "code" 就是那个占位符的名字。
	// 取不到时返回空字符串（不报错）。
	//
	// Java 对照：@PathVariable String code
	// 1.22 之前没有这个功能，大家要么用 gorilla/mux 这类第三方库，
	// 要么自己从 r.URL.Path 里手动切字符串。
	code := r.PathValue("code")

	link, ok := store.Get(code)
	if !ok {
		writeError(w, http.StatusNotFound, "link not found")
		return
	}

	store.IncrementHits(code)

	// 【302 还是 301？】这是个有实际后果的选择：
	//
	//   301 Moved Permanently —— 浏览器【永久缓存】这个跳转。
	//     下次访问短链时浏览器直接跳走，根本不会请求我们的服务器。
	//     好处是快、省流量；坏处是你再也统计不到这次访问，
	//     而且改了目标地址用户还是往老地方跳（缓存清不掉）
	//
	//   302 Found —— 临时跳转，每次都会回来问我们。
	//     短链服务几乎都用 302，就是要保留"每次都能统计 + 随时能改目标"的能力
	//
	// http.Redirect 会自动设置 Location 头 + 状态码，
	// 并且对 w 已经写过内容的情况做了防护。
	http.Redirect(w, r, link.URL, http.StatusFound)
}

// handleHealth 是健康检查端点。
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	// 第二个参数用 _ 占位 —— Go 里未使用的参数是允许的，
	// 但显式写 _ 能表达"我知道有这个参数，我不用"。
	//
	// 注意：未使用的【局部变量】是编译错误，未使用的【参数】不是。
	// 这个不对称经常让新手困惑。
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"links":  store.Len(),
	})
}

// ---------------------------------------------------------------------------
// 下面是两个响应辅助函数。
//
// Java 对照：类似 @ControllerAdvice 里统一封装 ResponseEntity 的那层。
// Go 里没有 AOP，这种横切逻辑就是【普通函数】，谁需要谁调用。
// 显式、啰嗦，但读代码时一眼能看到发生了什么。
// ---------------------------------------------------------------------------

// writeJSON 把 v 序列化成 JSON 写出去。
func writeJSON(w http.ResponseWriter, status int, v any) {
	// 先设 header 再写 status，顺序不能反：
	// WriteHeader 一旦调用，响应头就【锁定】了，之后再 Set 会被忽略，
	// Go 还会打一行 "superfluous response.WriteHeader call" 的警告。
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// 显式写状态码。
	// 如果省略，第一次往 w 写数据时会自动用 200。
	w.WriteHeader(status)

	// any 是 interface{} 的别名（Go 1.18+）。
	// Java 对照：Object。
	//
	// json.NewEncoder(w).Encode(v) 直接把 JSON 流式写进 w，
	// 不经过中间的 []byte —— 比 json.Marshal 再 w.Write 少一次分配。
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// 走到这里说明响应已经开始写了，状态码已经发出去了，
		// 没法再改成 500。只能记日志。
		//
		// 这也是 Go 和 Spring 的一个思维差异：
		// 响应一旦开始写就没法回头，所以错误处理要【前置】——
		// 先把所有可能失败的事情做完，最后再一次性写响应。
		log.Printf("writeJSON: %v", err)
	}
}

// writeError 返回一个 {"error": "..."} 格式的 JSON 错误。
func writeError(w http.ResponseWriter, status int, msg string) {
	// map[string]string 字面量，直接当 JSON 对象序列化。
	writeJSON(w, status, map[string]string{"error": msg})
}
```

### 重点讲解

#### ① 处理函数的签名是被接口定死的

```go
func handleCreateLink(w http.ResponseWriter, r *http.Request)
```

这个签名来自 `http.Handler` 接口：

```go
type Handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}
```

**三个必须记住的点**：

1. **没有返回值**——响应通过 `w` 写出去。不像 Spring 那样 `return` 一个对象让框架序列化
2. **`w` 在前，`r` 在后**——顺序是接口定的，写反了编译不过
3. **函数名小写**——它只是包内处理函数，不是给外部调的 API

#### ② `http.HandlerFunc` 的接口魔法

`mux.HandleFunc` 的第二个参数**不是** `Handler` 接口，而是 `func(w, r)`。怎么做到的？

标准库 `net/http/server.go` 里的真实代码（大约 10 行）：

```go
type HandlerFunc func(w http.ResponseWriter, r *http.Request)

func (f HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f(w, r)   // 方法体就是"调用自己"
}
```

**原理**：
- 普通函数 `func(w, r)` **不满足** `Handler` 接口（它没有方法）
- 但把它**转换**成 `HandlerFunc` 类型后，它就有了 `ServeHTTP` 方法 → **自动满足接口**
- 于是"函数"和"实现了接口的对象"之间的墙被打通了

`HandleFunc` 内部做的就是 `mux.Handle(pattern, HandlerFunc(handler))` 这个转换。

| | Java | Go |
|---|---|---|
| 函数变接口 | 需要 `@FunctionalInterface` 注解 + lambda/方法引用 | **方法集匹配即自动满足** |
| 声明 | 必须显式声明接口 | 无需声明 |

> **这 10 行是 M3 中间件的全部基础。** 中间件的签名就是：
> ```go
> func Logging(next http.Handler) http.Handler
> ```
> 接收一个 Handler，返回一个 Handler。

#### ③ JSON 解码的默认行为（和 Jackson 不同）

```go
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
```

Go 的默认行为：

| 情况 | Go 默认 | Jackson 默认 |
|---|---|---|
| 未知字段 | **忽略** | 通常报错（`FAIL_ON_UNKNOWN_PROPERTIES`） |
| 字段缺失 | **不报错**，保持零值 | 保持 null |
| 类型不匹配 | **报错** | 报错 |
| 大小写 | 不敏感（`Url` 也能匹配 `url`） | 敏感 |

想要严格模式得显式开：

```go
dec := json.NewDecoder(r.Body)
dec.DisallowUnknownFields()   // 有未知字段就报错
if err := dec.Decode(&req); err != nil { ... }
```

> 测试用例 `{"未知字段（Go 默认忽略）", ...}` 就是在验证默认行为。

**`&req` 为什么必须取地址**：`Decode` 要往变量里**写**数据，必须拿到它的地址。Java 里对象本来就是引用，没这个问题。

#### ④ `r.PathValue` —— Go 1.22 的新能力

```go
mux.HandleFunc("GET /{code}", handleRedirect)   // 注册
code := r.PathValue("code")                     // 取值
```

**1.22 之前**：要么引 `gorilla/mux`、`chi` 这类第三方库，要么自己从 `r.URL.Path` 里切字符串。

**路由优先级规则**：最具体的模式胜出。

```
GET /health     ← 更具体，优先
GET /{code}     ← 更宽泛
```

所以 `/health` 不会被 `/{code}` 抢走。而 `/api/links` 有两段，`/{code}` 只匹配**单段**路径，也不冲突。

#### ⑤ 302 vs 301 —— 一个真正有后果的选择

| | 301 Moved Permanently | 302 Found |
|---|---|---|
| 浏览器行为 | **永久缓存**跳转 | 每次都回来问服务器 |
| 服务器能否统计 | ❌ 统计不到 | ✅ 每一次都经过我们 |
| 能否改目标地址 | ❌ 用户还往老地方跳 | ✅ 随时能改 |
| 短链服务选哪个 | ❌ | ✅ **标准选择** |

> **为什么这是"有后果"的**：如果你用了 301，某天发现某条链接指向了钓鱼网站想改掉——**改不了**。用户浏览器里缓存的 301 会一直生效，你只能换一个新短码，但旧短码的流量你永远救不回来。

#### ⑥ 响应一旦开始写就不能回头

```go
w.Header().Set("Content-Type", "application/json; charset=utf-8")  // ① 先设 header
w.WriteHeader(status)                                              // ② 再写状态码
json.NewEncoder(w).Encode(v)                                       // ③ 最后写 body
```

**顺序不能反**：`WriteHeader` 一旦调用，响应头就**锁定**了。之后再 `Header().Set()` 会被静默忽略，Go 还会打印一行警告：

```
http: superfluous response.WriteHeader call from main.writeJSON
```

**推论：错误处理必须前置。**

```go
// ❌ 错误示范：写到一半才发现出错，已经救不回来了
json.NewEncoder(w).Encode(data)
if data.Invalid {
	writeError(w, http.StatusInternalServerError, "...")  // 没用，状态码早发出去了
}

// ✅ 正确：先把所有可能失败的事做完，最后一次性写响应
if err := validate(...); err != nil {
	writeError(w, http.StatusBadRequest, err.Error())
	return
}
writeJSON(w, http.StatusOK, data)
```

这和 Spring 里"随时 `throw`，让 `@ControllerAdvice` 兜底"是完全不同的思维方式。**Go 里没有兜底。**

#### ⑦ `any` 是 `interface{}` 的别名

```go
func writeJSON(w http.ResponseWriter, status int, v any)
```

`any` 是 Go 1.18 引入的 **`interface{}` 的别名**，两者完全等价（`any` 更好读）。

Java 对照：`Object`。

**但注意**：Go 的 `any` 和 Java 的 `Object` 有个关键差异——Go 1.18 之前**没有泛型**，所以 `any` 承担了所有泛型容器的角色，用起来到处都是类型断言。Java 里 `Object` 反而用得少，因为有泛型。

#### ⑧ `map[string]any` 直接当 JSON 对象

```go
writeJSON(w, http.StatusOK, map[string]any{
	"status": "ok",
	"links":  store.Len(),
})
```

Go 的 `encoding/json` 能直接序列化 `map[string]any`——不需要先定义 struct。

Java 对照：`Map.of("status", "ok", "links", n)`，Jackson 也能直接序列化。

**什么时候用哪个**：
- 响应结构固定 → 定义 struct（有类型检查，推荐）
- 临时的、简单的响应（比如错误）→ `map[string]any` 或 `map[string]string`

#### ⑨ 没有 AOP，横切逻辑就是普通函数

`writeJSON` / `writeError` 相当于 Spring 里统一封装 `ResponseEntity` 的那层。

**Go 里没有 AOP、没有 `@ControllerAdvice`。** 每个 handler 自己调用这些辅助函数——显式、啰嗦，但读代码时一眼能看到发生了什么。

> 真正的横切关注点（日志、鉴权、限流）在 Go 里用**中间件**实现——M3 的内容。
> 但即使是中间件，本质也只是"接收 Handler 返回 Handler 的普通函数"，不是什么框架魔法。

#### ⑩ 未使用的**参数**合法，未使用的**局部变量**是错误

```go
func handleHealth(w http.ResponseWriter, _ *http.Request) {   // ✅ 用 _ 占位
```

| | 未使用会怎样 |
|---|---|
| 局部变量 | **编译错误** `declared and not used` |
| 函数参数 | **合法**，不报错 |
| import | **编译错误** `imported and not used` |

这个不对称经常让新手困惑。惯例是：明知道不用但为了签名完整，就写 `_`。

---

## 7. 文件 5/6：`main.go` —— 组装与启动

### 完整源码

```go
// package 声明本文件属于哪个包。
//
// Java 对照：package com.example.shortlink;
//
//	→ Go 的包名就是一个【短单词】，不是域名倒写
//	→ main 包是特殊的：它编译成【可执行文件】而不是库
//	→ 同一个目录下所有 .go 文件必须属于同一个包
//	  （所以 M1 的 main.go / link.go / store.go / handler.go 都是 package main）
package main

// import 声明本文件用到的包。
//
// Java 对照：import java.util.List;
//
//	→ 没有通配符导入（没有 import java.util.*），必须逐个列出
//	→ 【致命差异】import 了却不用 = 编译错误，不是警告
//	  Java 里 unused import 只是 IDE 标灰，Go 里直接 build 失败
import (
	"log"
	"net"
	"net/http"
)

// store 是全局的内存存储。
//
// ⚠️ M1 【故意】这么写，M2 会把它改掉。它有三个真实问题：
//
//  1. 隐式依赖 —— 看 handleCreateLink 的函数签名，完全看不出它依赖 store。
//     Java 里你至少能在字段上看到 @Autowired private Store store;
//     而 Go 的包级变量是【完全隐式】的，读代码时容易漏
//
//  2. 无法隔离测试 —— 所有测试共享同一份数据，测试之间互相污染。
//     下面 handler_test.go 里每个用例都得手动 store = NewStore() 重置，
//     这就是坏味道
//
//  3. 无法替换实现 —— 想从内存换成 Redis，得改所有 handler
//
// Java 对照：相当于在每个 Service 里直接 new 一个 DAO，而不是注入。
//
// M2 会把它改成【显式依赖注入】：
//
//	type App struct { store *Store }
//	func (a *App) handleCreateLink(...) { a.store.Save(...) }
//
// 到那时你会发现，Go 的"显式优于隐式"不是口号，是被这种痛逼出来的。
var store = NewStore()

// main 是程序入口。
//
// Java 对照：public static void main(String[] args)
//
//	→ Go 的 main 无参数、无返回值
//	→ 没有类！main 是【包级函数】，不属于任何类型
//	→ 没有 static 关键字 —— 包级函数天然就是"静态"的
func main() {
	// 端口用 8090 —— 因为 8080 / 8081 / 8000 / 5173 都被本机其他项目占了。
	// 硬编码端口会撞车，这正是 M6 要引入配置管理的原因。
	addr := ":8090"

	// 先用 net.Listen 单独完成【绑定端口】这一步。
	//
	// 为什么不直接 http.ListenAndServe？—— 这不是风格问题，是实际踩到的 bug：
	//
	//	→ ListenAndServe 把"绑定端口"和"开始服务"揉成一个调用，
	//	  出错返回时分不清是绑定失败还是服务中途崩了。
	//	  于是日志打出了 "listening on :8090"，实际端口根本没绑上、
	//	  进程立刻就退了 —— 日志在骗人
	//	→ 拆成两步后，绑定成功才打日志，日志不会骗人
	//	→ 另外，拿到 ln 之后才有机会做优雅关闭（M6 的 srv.Shutdown）
	//
	// Java 对照：这一步相当于 Tomcat Connector 绑定端口。
	// Spring Boot 把它藏在自动配置里，Go 让你显式写出来。
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// log.Fatal = 打印错误 + os.Exit(1)，相当于 Java 的 System.exit(1)。
		// 注意：Fatal 会直接退出进程、【跳过所有 defer】，
		// 所以只适合在 main 顶层这么用，业务代码里绝对不要。
		log.Fatal(err)
	}

	// log.Printf 约等于 SLF4J 的 log.info()，但它是【标准库自带】的，
	// 不需要引任何依赖（M6 会换成结构化日志 log/slog）。
	//
	// ln.Addr() 返回【实际】绑定的地址 —— addr 写 ":8090"（不指定网卡）时，
	// 它会展开成 "[::]:8090"，让你看到真实情况。
	log.Printf("shortlink listening on %s", ln.Addr())

	// 开始服务并【阻塞】，直到出错退出。
	//
	// Java 对照：SpringApplication.run(App.class, args)
	//
	//	→ 不需要 new Thread / ExecutorService —— net/http 内部会为
	//	  每个请求起一个 goroutine。这是它和 Tomcat 线程池模型的根本区别
	//	→ 主 goroutine 就停在这里，整个进程靠它活着
	//
	// if err := f(); err != nil 是 Go 最经典的惯用法：
	//
	//	→ 把"声明 + 判空"压缩成一行
	//	→ err 的作用域【只在这个 if 内部】，出了 if 就不可见，
	//	  不污染外层命名空间
	if err := http.Serve(ln, routes()); err != nil {
		log.Fatal(err)
	}
}

// routes 注册所有路由，返回配置好的 mux。
//
// 把路由单独抽成函数，是为了让【测试】能拿到同一份路由表 ——
// 测试里不能去调 main()，那会真的监听端口。
// 这是 Go 里让 main 包可测试的标准做法。
func routes() *http.ServeMux {
	// http.NewServeMux() 约等于 Spring 的 DispatcherServlet。
	mux := http.NewServeMux()

	// 【Go 1.22+ 的路由语法】方法和路径写在同一个字符串里。
	// 1.22 之前只能写 "/health"，方法要自己在函数体里判断。
	//
	// Java 对照：@GetMapping("/health")
	mux.HandleFunc("GET /health", handleHealth)

	// Java 对照：@PostMapping("/api/links")
	mux.HandleFunc("POST /api/links", handleCreateLink)

	// {code} 是路径占位符，用 r.PathValue("code") 取值。
	// Java 对照：@GetMapping("/{code}") + @PathVariable
	//
	// 注意匹配优先级：Go 1.22 的 ServeMux 会选【最具体】的模式。
	// "GET /health" 比 "GET /{code}" 更具体，所以 /health 不会被它抢走。
	// 而 /api/links 有两段，"GET /{code}" 只匹配单段路径，也不会冲突。
	mux.HandleFunc("GET /{code}", handleRedirect)

	return mux
}
```

### 重点讲解

#### ① 那个全局 `var store` 是故意留的疤

```go
var store = NewStore()   // ⚠️
```

它有三个**真实**的问题：

**问题 1：隐式依赖**

看 `handleCreateLink` 的签名：

```go
func handleCreateLink(w http.ResponseWriter, r *http.Request)
```

**完全看不出它依赖 `store`。** 你得读函数体才知道。

| | Java | Go |
|---|---|---|
| 依赖在哪 | 字段上 `@Autowired private Store store;` | 可能藏在任何地方 |
| 可发现性 | 读类头部就看到了 | 得通读全文 |

**问题 2：测试互相污染**

因为所有测试共享同一个 `store`，`handler_test.go` 里每个用例都得手动重置：

```go
func TestCreateLink(t *testing.T) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store = NewStore()   // ← 每个用例都要写，很容易忘
			...
		})
	}
}
```

忘了就等着看莫名其妙的失败。

**问题 3：无法替换实现**

想从内存换成 Redis？得改**所有** handler。

**M2 的解法**：

```go
type App struct {
	store *Store      // 依赖是可见的字段
}

func (a *App) handleCreateLink(w http.ResponseWriter, r *http.Request) {
	...
	a.store.Save(link)   // 显式
}
```

到那时你会发现：**Go 的"显式优于隐式"不是口号，是被这种痛逼出来的。**

#### ② 为什么先 `net.Listen` 再 `http.Serve`

**这不是风格偏好，是实际踩到的 bug。**

`http.ListenAndServe(addr, handler)` 内部做两件事：**绑定端口** + **开始服务**。出错返回时只给你一个 `error`，你**分不清是哪一步失败的**。

实际踩坑场景：

```go
log.Printf("listening on %s", addr)        // 先打日志
if err := http.ListenAndServe(addr, mux); err != nil {
	log.Fatal(err)                          // 端口被占，绑定失败
}
```

日志打出了 `listening on :8090`，**但端口根本没绑上，进程立刻就退了**。同时因为 8090 上跑着别的服务，curl 还能收到响应——你会以为服务正常，实际上是**别人的服务在回你**。

**拆成两步**：

```go
ln, err := net.Listen("tcp", addr)          // ① 只做绑定
if err != nil {
	log.Fatal(err)                           // 绑定失败，这里就死了
}
log.Printf("listening on %s", ln.Addr())    // ② 绑定成功才打日志 —— 日志不骗人
http.Serve(ln, mux)                          // ③ 开始服务
```

**附加收益**：拿到 `ln` 之后才能做优雅关闭（M6 的 `srv.Shutdown(ctx)` 需要 `ln`）。

Java 对照：这一步相当于 Tomcat Connector 绑定端口。Spring Boot 把它藏在自动配置里，**Go 让你显式写出来**。

#### ③ `:8090` 里的冒号是什么

```go
addr := ":8090"
```

格式是 `[网卡地址]:端口`。**冒号前留空 = 监听所有网卡**（IPv4 的 `0.0.0.0` + IPv6 的 `::`）。

所以日志里 `ln.Addr()` 返回的是：

```
[::]:8090
```

`[::]` 是 IPv6 的通配地址，同时接受 IPv4 连接（取决于系统配置）。

想要只监听本机（不对外暴露）：

```go
addr := "127.0.0.1:8090"
```

#### ④ `log.Fatal` 的陷阱

```go
log.Fatal(err)
```

= `log.Print(err)` + `os.Exit(1)`。

**注意**：`os.Exit` 会**跳过所有 `defer`**。

```go
func main() {
	defer cleanup()          // ❌ 不会被调用
	if err := run(); err != nil {
		log.Fatal(err)       // 直接退出，cleanup 被跳过
	}
}
```

所以 `log.Fatal` **只适合用在 `main` 顶层**。业务代码里要用 `return err` 把错误传上去。

Java 对照：`System.exit(1)`。

#### ⑤ Go 的编译产物不需要运行时

```bash
go build -o shortlink.exe .
# → 8.3 MB 单个文件
```

| | Go | Spring Boot |
|---|---|---|
| 产物 | 8.3 MB 单文件 | ~20 MB fat jar |
| 运行时 | **无**，静态链接 | 需要 JRE（~200 MB） |
| 启动 | 毫秒级 | 秒级 |
| 部署 | 拷贝一个文件 | 装 JDK + 传 jar + `java -jar` |

8.3 MB 里包含了 HTTP 服务器、TLS 实现、GC、调度器、反射。这就是 Go 在部署上的核心优势，**M6 打 Docker 镜像时会用到**（可以做出 ~10 MB 的镜像）。

> 还可以进一步瘦身：
> ```bash
> go build -ldflags="-s -w" -o shortlink.exe .   # 去掉符号表和调试信息
> ```
> 能减到 ~5.5 MB。代价是不能用 `dlv` 调试了，所以开发时别加。

#### ⑥ 不需要线程池

```go
http.Serve(ln, mux)   // 阻塞主 goroutine
```

`net/http` 内部**为每个请求起一个 goroutine**。

| | Java 线程 | Go goroutine |
|---|---|---|
| 初始栈 | ~1 MB | **2 KB** |
| 数量级 | 数千 | **数十万** |
| 调度 | OS 内核 | Go runtime（GMP，M:N 模型） |
| 需要池化 | **必需** | **不需要** |
| 创建开销 | 高（系统调用） | 极低（用户态） |

Java 里你必须用 `ExecutorService` 限制线程数，否则 OOM。Go 里直接 `go f()` 就行——runtime 会处理调度。

**这是 Go 在 IO 密集型服务上的核心优势。**

#### ⑦ 为什么把路由抽成 `routes()` 函数

```go
func routes() *http.ServeMux {
	...
}
```

**因为测试不能调 `main()`** ——那会真的去监听端口。

抽出来之后，测试可以：

```go
mux := routes()
mux.ServeHTTP(rec, req)    // 直接调，不经过网络
```

**这是 Go 里让 `main` 包可测试的标准做法。** 更复杂的项目会把它拆到 `internal/router` 包里（M2 会做）。

---

## 8. 文件 6/6：`handler_test.go` —— 测试

### 完整源码

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ⚠️ 注意每个测试开头都有一句 store = NewStore()。
//
// 这就是 main.go 里说的"全局变量让测试难写"的具体表现：
// 测试之间共享同一份数据，必须手动重置才不会互相污染。
// 而且你【很容易忘】—— 忘了就会看到莫名其妙的失败。
//
// M2 改成依赖注入后，这句就没有了：每个测试 new 一个自己的 App。
//
// 另外，Go 的测试文件命名规则：
//   - 必须以 _test.go 结尾
//   - 【和被测试的代码放在同一个目录、同一个包】
//   - Java 对照：不是 src/test/java 和 src/main/java 分开的两棵树
//   - 好处是测试能访问包内私有成员（比如这里的 store 和 generateCode）
//   - 编译产物里不会包含 _test.go，不用担心体积
//
// 运行方式：
//
//	go test .                     # 当前包
//	go test -v .                  # 显示每个用例
//	CGO_ENABLED=1 go test -race .  # 带竞态检测

// ---------------------------------------------------------------------------
// 单元测试：短码生成
// ---------------------------------------------------------------------------

func TestGenerateCode(t *testing.T) {
	// 生成一批，验证长度、字符集、以及"看起来"不重复
	seen := make(map[string]bool)
	const n = 1000

	for range n {
		code := generateCode()

		if len(code) != CodeLen {
			t.Fatalf("短码长度 = %d, 期望 %d", len(code), CodeLen)
		}

		for _, c := range code {
			if !strings.ContainsRune(CodeAlphabet, c) {
				t.Fatalf("短码 %q 含有非法字符 %q", code, c)
			}
		}

		// 注意：这里【没有】断言"不重复"。
		// 因为随机生成【理应有碰撞可能】，1000 个里撞一两个不算 bug。
		// 断言不重复的测试是错的 —— 它在概率上偶尔会挂。
		seen[code] = true
	}

	// 只做一个宽松的健全性检查：1000 个里至少要有 990 个不同的。
	// 如果大量重复，说明随机源有问题。
	if len(seen) < 990 {
		t.Errorf("1000 次生成只有 %d 个不同短码，随机性可疑", len(seen))
	}
}

func TestValidateURL(t *testing.T) {
	// 【表驱动测试】—— Go 社区的标准写法。
	//
	// 定义一个匿名 struct 的切片，每个元素是一个用例。
	// Java 对照：JUnit 5 的 @ParameterizedTest + @MethodSource，
	// 但 Go 不需要任何注解和框架，就是普通的切片 + for 循环。
	//
	// 为什么用匿名 struct？因为它只用这一次，起个名字反而啰嗦。
	tests := []struct {
		name    string // 用例名，会出现在失败信息里
		url     string
		wantErr bool
	}{
		{"https 正常", "https://go.dev", false},
		{"http 正常", "http://example.com/a/b?c=d", false},
		{"带端口", "https://example.com:8090/x", false},
		{"缺 scheme", "example.com", true},
		{"错 scheme", "ftp://example.com", true},
		{"危险 scheme", "javascript:alert(1)", true},
		{"只有 scheme", "https://", true},
		{"空字符串", "", true},
	}

	for _, tt := range tests {
		// t.Run 创建一个子测试，名字会显示在输出里。
		//
		// Go 1.22 之前这里必须写 tt := tt 才能安全并发；
		// 1.22 起每轮循环都有独立的 tt，不用了。
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL(tt.url)

			// 判断错误是不是"那个特定的错误"，用 errors.Is 而不是 ==。
			// 这里为了简单直接判空，真实项目里应该用 errors.Is(err, ErrInvalidURL)。
			if (err != nil) != tt.wantErr {
				t.Errorf("validateURL(%q) 返回 err=%v, 期望 wantErr=%v", tt.url, err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HTTP 层测试：表驱动
// ---------------------------------------------------------------------------

func TestCreateLink(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"合法 https", `{"url":"https://go.dev"}`, http.StatusCreated},
		{"合法 http", `{"url":"http://example.com/a/b"}`, http.StatusCreated},
		{"缺 scheme", `{"url":"example.com"}`, http.StatusBadRequest},
		{"空 URL", `{"url":""}`, http.StatusBadRequest},
		{"非法 JSON", `{这不是 JSON}`, http.StatusBadRequest},
		{"JSON 数组而非对象", `["https://go.dev"]`, http.StatusBadRequest},
		{"未知字段（Go 默认忽略）", `{"url":"https://go.dev","extra":123}`, http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store = NewStore() // 重置全局状态 —— 见文件开头的说明

			// httptest.NewRequest 造一个请求，不经过真实网络。
			// Java 对照：MockMvc 的 post("/api/links").content(...)
			//
			// strings.NewReader 把字符串包成 io.Reader，
			// 这样它就能当请求体 —— Go 的接口让"字符串"变成了"可读流"。
			req := httptest.NewRequest(http.MethodPost, "/api/links", strings.NewReader(tt.body))

			// httptest.NewRecorder 是 ResponseWriter 的一个假实现，
			// 把响应录下来供断言。
			// Java 对照：MockHttpServletResponse
			rec := httptest.NewRecorder()

			// 直接调用路由，不启服务器 —— 所以测试跑得飞快。
			routes().ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("状态码 = %d, 期望 %d\n响应体: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 端到端流程测试：创建 → 跳转
// ---------------------------------------------------------------------------

func TestCreateThenRedirect(t *testing.T) {
	store = NewStore()
	mux := routes()

	// --- 第一步：创建 ---
	createReq := httptest.NewRequest(http.MethodPost, "/api/links",
		strings.NewReader(`{"url":"https://go.dev/ref/spec"}`))
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("创建失败，状态码 = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	// 解析响应，拿到生成的短码
	var created Link
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if created.Code == "" {
		t.Fatal("响应里没有 code")
	}
	if created.Hits != 0 {
		t.Errorf("新建链接的 Hits = %d, 期望 0", created.Hits)
	}

	// --- 第二步：跳转 ---
	redirectReq := httptest.NewRequest(http.MethodGet, "/"+created.Code, nil)
	redirectRec := httptest.NewRecorder()
	mux.ServeHTTP(redirectRec, redirectReq)

	if redirectRec.Code != http.StatusFound {
		t.Errorf("状态码 = %d, 期望 %d (302)", redirectRec.Code, http.StatusFound)
	}

	// 断言 Location 头
	if got := redirectRec.Header().Get("Location"); got != "https://go.dev/ref/spec" {
		t.Errorf("Location = %q, 期望 %q", got, "https://go.dev/ref/spec")
	}

	// --- 第三步：验证访问计数 +1 ---
	link, ok := store.Get(created.Code)
	if !ok {
		t.Fatal("跳转后链接应该还在")
	}
	if link.Hits != 1 {
		t.Errorf("Hits = %d, 期望 1（IncrementHits 里漏写 s.links[code] = l？）", link.Hits)
	}
}

func TestRedirectNotFound(t *testing.T) {
	store = NewStore()

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()
	routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("状态码 = %d, 期望 404", rec.Code)
	}
}

func TestHealth(t *testing.T) {
	store = NewStore()
	store.Save(Link{Code: "aaa111", URL: "https://go.dev"})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	// /health 不能被 /{code} 抢走 —— 这就是路由优先级的验证
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, 期望 ok —— 路由可能被 GET /{code} 抢走了", body["status"])
	}
}

// ---------------------------------------------------------------------------
// 并发测试 —— 这个测试的意义超出"验证功能正确"
// ---------------------------------------------------------------------------

// TestConcurrentCreate 并发创建 200 条链接。
//
// 【重点】这个测试不加 -race 也能过，但它真正的价值在于：
//
//	CGO_ENABLED=1 go test -race -run TestConcurrentCreate .
//
// 加了 -race 之后，它会去检查 store 的加锁是否真的正确。
// 如果把 Store.mu 去掉，或者把 RWMutex 换成普通 map 不加锁，
// 这个测试会立刻报 fatal error: concurrent map writes，进程直接挂。
//
// 这就是为什么 M1 要专门写一个并发测试：
// 让你亲眼看到"锁不是装饰品"，以及 -race 到底在防什么。
func TestConcurrentCreate(t *testing.T) {
	store = NewStore()
	mux := routes()

	const n = 200

	// WaitGroup 是 Go 的 CountDownLatch。
	// Java 对照：CountDownLatch / ExecutorService.invokeAll
	var wg sync.WaitGroup

	for range n {
		// 【必须在 go 之前调用 Add】
		// 如果写在 goroutine 内部，可能出现 wg.Wait() 已经返回了、
		// 某个 goroutine 才刚开始执行 Add 的情况 —— 经典的竞态 bug。
		wg.Add(1)

		// go 关键字启动一个 goroutine。
		// Java 对照：executor.submit(() -> {...})
		// 但不需要线程池、不需要 Future、开销极小（初始栈 2KB）
		go func() {
			// defer 保证无论函数怎么返回都会 Done，
			// 即使中间 panic 了也一样。Java 对照：finally { latch.countDown(); }
			defer wg.Done()

			req := httptest.NewRequest(http.MethodPost, "/api/links",
				strings.NewReader(`{"url":"https://go.dev"}`))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusCreated {
				// 【重要】goroutine 里不能用 t.Fatalf。
				//
				// t.Fatalf 会调用 runtime.Goexit() 终止【当前 goroutine】，
				// 而当前是子 goroutine，测试主 goroutine 不会被终止，
				// 只会导致 wg.Done() 不被执行（defer 倒是会跑）和诡异的输出。
				// 所以子 goroutine 里一律用 t.Errorf。
				t.Errorf("并发创建失败，状态码 = %d", rec.Code)
			}
		}()
	}

	// 等所有 goroutine 完成
	wg.Wait()

	// 200 次创建，应该正好 200 条。
	// 如果 RWMutex 写错了（比如某处用 RLock 却做了写操作），
	// 这里就会少 —— 而且不加 -race 的话你看不出原因。
	if got := store.Len(); got != n {
		t.Errorf("并发创建后 Len() = %d, 期望 %d —— 有写入丢失，检查锁", got, n)
	}
}

// TestConcurrentRedirect 并发跳转同一个短码，验证计数不丢。
func TestConcurrentRedirect(t *testing.T) {
	store = NewStore()
	mux := routes()

	store.Save(Link{Code: "hot123", URL: "https://go.dev"})

	const n = 200
	var wg sync.WaitGroup

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/hot123", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusFound {
				t.Errorf("跳转失败，状态码 = %d", rec.Code)
			}
		}()
	}

	wg.Wait()

	link, _ := store.Get("hot123")
	if link.Hits != n {
		t.Errorf("Hits = %d, 期望 %d —— 计数丢失", link.Hits, n)
	}
}
```

### 重点讲解

#### ① Go 测试的三条规则

1. 文件名必须以 **`_test.go`** 结尾
2. **和被测代码放在同一目录、同一个包**（不是 `src/test/java` 那种两棵树）
3. 测试函数必须是 `func TestXxx(t *testing.T)`，`Test` 后第一个字母**必须大写**

| | Java | Go |
|---|---|---|
| 位置 | `src/test/java/...` | 同目录 `xxx_test.go` |
| 框架 | JUnit 5（第三方） | **`testing` 标准库** |
| 断言 | `assertEquals(exp, act)` | **手写 `if`** |
| 参数化 | `@ParameterizedTest` | 匿名 struct 切片 + `for` |
| Mock | Mockito | `httptest` + 手写 fake |
| 运行 | `mvn test` | `go test ./...` |

**同包的好处**：测试能访问包内私有成员——`TestGenerateCode` 直接调了未导出的 `generateCode()`，`TestCreateThenRedirect` 直接读了全局 `store`。

**`_test.go` 不会进编译产物**，所以不用担心体积。

#### ② 表驱动测试（Table-Driven Test）

**这是 Go 社区最重要的测试惯例**，几乎每个 Go 项目的测试都是这个形状：

```go
tests := []struct {
	name    string
	url     string
	wantErr bool
}{
	{"https 正常", "https://go.dev", false},
	{"缺 scheme", "example.com", true},
	// ...
}

for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) {
		err := validateURL(tt.url)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateURL(%q) 返回 err=%v, 期望 wantErr=%v", tt.url, err, tt.wantErr)
		}
	})
}
```

**为什么用匿名 struct**：它只用这一次，起个名字（比如 `type urlTestCase struct`）反而啰嗦。

**`t.Run(name, fn)` 创建子测试**，好处是：
- 失败信息里能看到是哪个用例挂了
- `go test -run 'TestValidateURL/危险'` 能只跑匹配的子测试
- 可以用 `t.Parallel()` 让子测试并发跑

#### ③ Go 1.22 的循环变量修复

```go
for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) {   // 闭包捕获 tt
		use(tt)
	})
}
```

**Go 1.22 之前**，这里的 `tt` 是**整个循环共用一个变量**。闭包捕获的是变量本身，所以所有子测试可能都读到**最后一个** `tt`。

老代码里必须写：

```go
for _, tt := range tests {
	tt := tt   // ← 手动创建每轮独立的副本
	t.Run(tt.name, func(t *testing.T) { ... })
}
```

**Go 1.22 起，`for` 循环每轮都创建新变量**，这个坑没有了。但你在老项目里还会大量见到 `tt := tt` 这行——现在你知道它是干嘛的了。

#### ④ `httptest` 替代 MockMvc

| Java / Spring | Go |
|---|---|
| `MockMvc mockMvc` | `routes()` 返回的 `*http.ServeMux` |
| `MockMvcRequestBuilders.post("/x")` | `httptest.NewRequest("POST", "/x", body)` |
| `MockHttpServletResponse` | `httptest.NewRecorder()` |
| `mockMvc.perform(req)` | `mux.ServeHTTP(rec, req)` |
| `result.getResponse().getStatus()` | `rec.Code` |
| `result.getResponse().getContentAsString()` | `rec.Body.String()` |
| `result.getResponse().getHeader("X")` | `rec.Header().Get("X")` |

**关键点：不启服务器。** `mux.ServeHTTP(rec, req)` 直接调用，不经过网络、不占端口——所以整个测试套件跑完只要 **0.5 秒**。

`httptest.NewRecorder()` 是 `http.ResponseWriter` 接口的一个**假实现**，把响应记在内存里供你断言。

#### ⑤ `strings.NewReader` 把字符串变成请求体

```go
strings.NewReader(`{"url":"https://go.dev"}`)
```

`http.Request.Body` 的类型是 `io.ReadCloser`，而 `strings.NewReader` 返回的 `*strings.Reader` **满足 `io.Reader`**——所以能直接传进去。

**这就是 Go 接口的价值**：`"字符串"` → `"可读流"` 不需要任何适配器。

Java 里你得：`new ByteArrayInputStream(json.getBytes(StandardCharsets.UTF_8))`

#### ⑥ `t.Fatalf` vs `t.Errorf`

| | `t.Fatalf` | `t.Errorf` |
|---|---|---|
| 记录失败 | ✅ | ✅ |
| 终止测试 | ✅ **立即** | ❌ 继续执行 |
| 实现 | `runtime.Goexit()` | 只是标记 |
| 用在哪 | **主 goroutine** | 任何地方 |

**规则**：`t.Fatalf` 会调 `runtime.Goexit()`，**只终止当前 goroutine**。在子 goroutine 里调用，测试主 goroutine 不会停——你会得到诡异的输出和挂起的测试。

**所以：子 goroutine 里一律用 `t.Errorf`。**

#### ⑦ `wg.Add(1)` 必须在 `go` 之前

```go
// ✅ 正确
wg.Add(1)
go func() {
	defer wg.Done()
	...
}()

// ❌ 错误
go func() {
	wg.Add(1)      // 可能 wg.Wait() 已经返回了才执行到这
	defer wg.Done()
	...
}()
```

错误写法下，`wg.Wait()` 可能在所有 goroutine 执行 `Add` 之前就返回了（因为计数器还是 0）——测试**提前结束**，goroutine 还在跑。这是经典竞态 bug。

**规则：`Add` 在 `go` 之前调用。**

#### ⑧ 并发测试的真正价值

`TestConcurrentCreate` 和 `TestConcurrentRedirect` **不加 `-race` 也能过**。它们的价值在于配合 `-race`：

```bash
CGO_ENABLED=1 go test -race -count=1 .
```

**试试看**：把 `Store.mu` 删掉，或者把 `links` 改成 `map[string]*Link` 且 `Get` 直接返回指针，然后：

```bash
CGO_ENABLED=1 go test -race .
```

你会看到：

```
==================
WARNING: DATA RACE
Write at 0x00c0000b4010 by goroutine 8:
  main.(*Store).IncrementHits()
...
Previous read at 0x00c0000b4010 by goroutine 7:
  main.handleRedirect()
==================
```

或者：

```
fatal error: concurrent map writes
```

**这就是为什么 M1 要专门写并发测试**——让你亲眼看到「锁不是装饰品」，以及 `-race` 到底在防什么。

> **恢复代码**：改坏了记得改回来，或者 `git checkout store.go`。

---

## 9. 跑起来验证

全部文件敲完后，依次执行：

### 9.1 格式化

```bash
gofmt -l .
```

**无输出 = 格式正确**。有输出的话列出的是不合规的文件名，直接修：

```bash
gofmt -w .
```

> `gofmt` 没有配置项——**Go 社区不为代码风格争论**，这是和 Java 里 Checkstyle 配置打架完全不同的体验。
> VS Code 里配了 `editor.formatOnSave` + `goimports` 后，保存时自动完成。

### 9.2 编译

```bash
go build -o shortlink.exe .
```

无输出 = 成功。产物大小约 8.3 MB。

### 9.3 测试

```bash
go test -v .
```

期望输出：

```
=== RUN   TestGenerateCode
--- PASS: TestGenerateCode (0.00s)
=== RUN   TestValidateURL
=== RUN   TestValidateURL/https_正常
--- PASS: TestValidateURL/https_正常 (0.00s)
... （每个子测试一行）
--- PASS: TestValidateURL (0.00s)
=== RUN   TestCreateLink
...
PASS
ok  	shortlink	0.514s
```

### 9.4 竞态检测

```bash
CGO_ENABLED=1 go test -race -count=1 .
```

期望输出：

```
ok  	shortlink	1.591s
```

**没有任何 `WARNING: DATA RACE` 就是对的。**

> ⚠️ **Windows 上 `-race` 需要 gcc 在 PATH 里**，否则会报 `FAIL shortlink [build failed]`。
> 确认：`gcc --version` 能输出版本号。不能的话把 mingw 的 `bin` 目录加进 PATH。

### 9.5 启动服务

```bash
./shortlink.exe
```

期望输出：

```
2026/09/28 17:15:31 shortlink listening on [::]:8090
```

### 9.6 手工验证五个接口

**另开一个终端**：

```bash
# ① 健康检查
curl -i http://127.0.0.1:8090/health
# HTTP/1.1 200 OK
# {"links":0,"status":"ok"}

# ② 创建短链
curl -i -X POST http://127.0.0.1:8090/api/links \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://go.dev/ref/spec"}'
# HTTP/1.1 201 Created
# {"code":"aB3xK9","url":"https://go.dev/ref/spec","created_at":"...","hits":0}

# ③ 跳转（复制上一步返回的 code；注意【不要】加 -L，否则会跟随跳转看不到 302）
curl -i http://127.0.0.1:8090/aB3xK9
# HTTP/1.1 302 Found
# Location: https://go.dev/ref/spec

# ④ 再跳一次，然后看计数变成 2
curl -s -o /dev/null http://127.0.0.1:8090/aB3xK9
curl -s http://127.0.0.1:8090/health
# {"links":1,"status":"ok"}
curl -s -i http://127.0.0.1:8090/aB3xK9 | head -3    # 还是 302

# ⑤ 不存在的短码
curl -i http://127.0.0.1:8090/nonexistent
# HTTP/1.1 404 Not Found
# {"error":"link not found"}

# ⑥ 非法 URL
curl -i -X POST http://127.0.0.1:8090/api/links \
  -H 'Content-Type: application/json' \
  -d '{"url":"example.com"}'
# HTTP/1.1 400 Bad Request
# {"error":"url must start with http:// or https://"}

# ⑦ 危险 scheme
curl -s -X POST http://127.0.0.1:8090/api/links \
  -H 'Content-Type: application/json' \
  -d '{"url":"javascript:alert(1)"}'
# {"error":"url must start with http:// or https://"}

# ⑧ 方法不匹配
curl -i -X POST http://127.0.0.1:8090/health
# HTTP/1.1 405 Method Not Allowed
```

### 9.7 全部通过的标准

| 检查项 | 期望 |
|---|---|
| `gofmt -l .` | 无输出 |
| `go build` | 无错误 |
| `go test -v .` | `PASS` + `ok shortlink` |
| `CGO_ENABLED=1 go test -race .` | `ok`，**无 DATA RACE** |
| `GET /health` | `200` |
| `POST /api/links` | `201` + 短码 |
| `GET /{code}` | `302` + 正确 Location |
| `GET /nonexistent` | `404` |
| `POST` 非法 URL | `400` |
| `POST /health` | `405` |

**全绿 → M1 通关，可以进 M2。**

---

## 10. Java ↔ Go 对照总表

| 概念 | Java / Spring Boot | Go | 备注 |
|---|---|---|---|
| 构建文件 | `pom.xml` | `go.mod` | Go 的只有 3 行 |
| 依赖锁 | `package-lock.json`（npm 类比） | `go.sum` | **必须提交** |
| 包声明 | `package com.example.x;` | `package main` | 短单词，非域名 |
| 导入 | `import java.util.List;` | `import "net/http"` | **不用即编译错误** |
| 通配符导入 | `import java.util.*;` | **不存在** | |
| 入口 | `public static void main(String[])` | `func main()` | 无参数无返回值 |
| 类 | `class Link` | `type Link struct` | 没有类，只有 struct |
| 构造器 | `public Link(...)` | `func NewLink(...) *Link` | **只是命名约定** |
| getter/setter | Lombok `@Data` | **直接导出字段** | 首字母大小写控制可见性 |
| 可见性 | `public/private/protected` | 首字母大小写 + `internal/` | **没有 protected** |
| 序列化 | Jackson `@JsonProperty` | struct tag `` `json:"x"` `` | tag 不是注解 |
| 常量 | `static final int N = 6;` | `const N = 6` | Go 常量**无类型** |
| 枚举 | `enum Status { A, B }` | `const` + `iota` | 没有类型安全 |
| 自定义异常 | `class XException extends ...` | `var ErrX = errors.New("...")` | 错误是**值** |
| 抛出 | `throw new XException()` | `return ErrX` | 没有 throw |
| 包装错误 | `new XException("ctx", cause)` | `fmt.Errorf("ctx: %w", err)` | `%w` 保留错误链 |
| 捕获 | `try / catch / finally` | `if err != nil` / `defer` | |
| 匹配错误类型 | `catch (XException e)` | `errors.Is(err, ErrX)` | |
| 提取错误详情 | `(XException) e` | `errors.As(err, &target)` | |
| 判空 | `Optional<T>` / `null` | `(T, bool)` comma-ok | |
| map | `HashMap<K,V>` | `map[K]V` | |
| 并发 map | `ConcurrentHashMap` | 普通 map + `sync.RWMutex` | **`sync.Map` 是特例工具** |
| 并发 map 写坏 | 数据错乱、死循环 | **进程立即崩溃** | Go 严重得多 |
| 读写锁 | `ReentrantReadWriteLock` | `sync.RWMutex` | **零值可用** |
| 原子类 | `AtomicLong` | `atomic.Int64` | |
| 起并发任务 | `executor.submit(r)` | `go f()` | **无需线程池** |
| 等待完成 | `CountDownLatch` | `sync.WaitGroup` | `Add` 必须在 `go` 前 |
| 线程本地 | `ThreadLocal` | **不存在** | 用 `context.Context`（M3） |
| 任务队列 | `BlockingQueue<T>` | `chan T` | M3/M4 内容 |
| 控制器 | `@RestController` | 普通函数 | |
| 路由 | `@GetMapping("/x")` | `mux.HandleFunc("GET /x", fn)` | 方法写在路径里（1.22+） |
| 路径参数 | `@PathVariable` | `r.PathValue("code")` | 1.22+ |
| 请求体 | `@RequestBody DTO` | `json.NewDecoder(r.Body).Decode(&v)` | 默认忽略未知字段 |
| 响应 | `return ResponseEntity<T>` | `w.WriteHeader()` + `Encode(v)` | **没有返回值** |
| 过滤器 | `Filter` / AOP / `@ControllerAdvice` | 中间件函数 | M3 内容 |
| 依赖注入 | `@Autowired` + IoC 容器 | **手写 `NewXxx(dep)`** | M2 内容 |
| HTTP 测试 | `MockMvc` | `httptest` | 都不启服务器 |
| 参数化测试 | `@ParameterizedTest` | 匿名 struct 切片 + `for` | |
| 断言 | `assertEquals` | 手写 `if` + `t.Errorf` | 无断言库 |
| 测试位置 | `src/test/java` | 同目录 `_test.go` | |
| 测试运行 | `mvn test` | `go test ./...` | |
| 竞态检测 | 无对应物 | `-race` | Go 独有 |
| 产物 | `app.jar` + JRE（~220 MB） | 单文件 8.3 MB | **无运行时依赖** |
| 启动 | 秒级 | **毫秒级** | |

---

## 11. 七个关键知识点

### 1. 字段可见性靠首字母大小写

```go
type Link struct {
	Code string // 导出 ≈ public
	url  string // 不导出 ≈ package-private
}
```

**没有 `protected`**，因为 Go 没有继承。

**封装靠包（package）而不是靠类。** 跨模块的"受保护"靠 `internal/` 目录（M2 会用到）——这是**编译器强制**的，比 Java 的包命名约定硬得多。

### 2. 值语义贯穿一切

```go
l, ok := s.links[code]   // l 是【拷贝】
l.Hits++
s.links[code] = l        // 不写回 = 白改
```

**Java 开发者 100% 会漏最后一行**，因为 Java 的 `map.get()` 返回对象引用。

而正因为是值语义，`Get` 返回拷贝就**天然消除了共享可变状态**——这是 Go 里防数据竞争最有力的手段。

| 类型 | 赋值/传参行为 |
|---|---|
| `int` `string` `bool` `struct` `[N]T` | **深拷贝** |
| `slice` `map` `chan` `func` `interface` `*T` | 拷贝"描述符"，底层数据共享 |

### 3. Go 的 map 不是并发安全的

并发写 → **`fatal error: concurrent map writes`** → **进程立即死亡**，`recover` 救不回来。

| | Java `HashMap` | Go `map` |
|---|---|---|
| 并发写 | 数据错乱、可能死循环 | **进程崩溃** |
| 检测 | 无 | runtime 内置检测 |

**只要有多个 goroutine 碰同一个 map，就必须加锁。没有例外。**

### 4. `http.Handler` 就一个方法

```go
type Handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type HandlerFunc func(w http.ResponseWriter, r *http.Request)

func (f HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f(w, r)   // 方法体就是"调用自己"
}
```

**这 10 行是 M3 中间件的全部基础。**

- Java 里"函数变接口"需要 `@FunctionalInterface` 注解
- Go 里**方法集匹配就自动满足接口**，无需任何声明

### 5. `if err := f(); err != nil` 是 Go 的呼吸方式

```go
if err := validateURL(req.URL); err != nil {
	writeError(w, http.StatusBadRequest, err.Error())
	return
}
```

- 声明和判空压成一行
- `err` 作用域**只在这个 if 内**，不污染外层命名空间
- 你会写几千遍这个模式

**错误是值，不是控制流。** 没有 `throw`，没有隐式的栈展开。

### 6. 响应一旦开始写就不能回头

```go
w.Header().Set(...)    // ① 先 header
w.WriteHeader(status)  // ② 再状态码 —— 之后 header 锁定
json.NewEncoder(w).Encode(v)  // ③ 最后 body
```

**推论：错误处理必须前置。**

```go
// ✅ 先把所有可能失败的事做完，最后一次性写响应
if err := validate(...); err != nil {
	writeError(w, http.StatusBadRequest, err.Error())
	return
}
writeJSON(w, http.StatusOK, data)
```

这和 Spring 里"随时 `throw`，`@ControllerAdvice` 兜底"**完全相反**。Go 里没有兜底。

### 7. 不需要线程池

`net/http` **为每个请求起一个 goroutine**。

| | Java 线程 | Go goroutine |
|---|---|---|
| 初始栈 | ~1 MB | **2 KB** |
| 数量级 | 数千 | **数十万** |
| 调度 | OS 内核 | runtime（GMP，M:N） |
| 池化 | **必需** | **不需要** |

Java 里不用线程池会 OOM；Go 里直接 `go f()` 就行。

---

## 12. 踩坑记录

### 1. 端口 8080 被占用

本机 8080 / 8081 / 8000 / 5173 都被其他项目占着。M1 用 **8090**。

**硬编码端口会撞车**——这正是 M6 要引入配置管理（环境变量 / 命令行参数）的原因。

排查端口占用：

```bash
# Windows
netstat -ano | findstr :8080
# 拿到 PID 后
tasklist | findstr <PID>
```

### 2. `http.ListenAndServe` 会打出骗人的日志

**现象**：日志显示 `listening on :8080`，但请求全返回 500。那些 500 其实是**别的进程**返回的——我们的进程早就因为绑定失败退出了。

**原因**：`ListenAndServe` 把"绑定端口"和"开始服务"揉成一个调用，出错时分不清是哪一步。

**修复**：拆成 `net.Listen` + `http.Serve` 两步（见 `main.go`）。

### 3. Windows 上 `-race` 需要 gcc 在 PATH 里

```bash
# ❌ 只把 go 加进 PATH
CGO_ENABLED=1 go test -race .
# FAIL	shortlink [build failed]

# ✅ 必须有 gcc
export PATH="/d/Dev/mingw64/bin:/d/Dev/go/bin:$PATH"
CGO_ENABLED=1 go test -race .
# ok  	shortlink	1.591s
```

**注意**：全局保持 `CGO_ENABLED=0`，让 `go build` 继续产出**纯 Go 静态单文件**（M6 打 Docker 镜像靠它做到 ~10 MB）。**只在测试时开 cgo。**

### 4. 空 `.go` 文件不是合法 Go

一个 0 字节的 `test.go` 会报错——缺 `package` 声明。

而且**文件必须属于某个 module**（所在目录或上级有 `go.mod`），否则 gopls 降级工作，补全/跳转/格式化都打折扣。

### 5. 忘记声明变量

```go
// ❌ 编译错误：undefined: req
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {

// ✅
var req createLinkRequest
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
```

用 `:=` 一步到位看着更短，但后面还要用 `req.URL`，所以必须提前声明。

### 6. struct tag 写错编译器不管

```go
Code string `json:"code"`    // ✅
Code string `json:code`      // ❌ 编译能过，但序列化字段名不对
```

**改完 tag 一定要 curl 验证一下**，别只看编译通过。

### 7. `nil map` 写入会 panic

```go
var m map[string]int
m["a"] = 1        // panic: assignment to entry in nil map

m := make(map[string]int)   // ✅ 必须 make
m["a"] = 1
```

但 **`nil map` 读取是安全的**（返回零值），`len(nil map)` 也是 0。

> `nil slice` 更宽松：可以 `append`、`range`、`len`，都安全。只有 map 有这个问题。

---

## 13. 自测题（含答案）

先自己答，再看答案。

---

**Q1.** 把 `mux.HandleFunc("GET /health", ...)` 改成 `mux.HandleFunc("/health", ...)`（去掉 `GET `），`POST /health` 会返回什么？

<details>
<summary>答案</summary>

返回 **`200`**。因为 `/health` 不限定方法，任何 HTTP 方法都能匹配。

原来的 `"GET /health"` 限定方法后，`POST /health` 返回 **`405 Method Not Allowed`**——这是 Go 1.22 的 `ServeMux` **自动**做的，不需要你手写判断。

Java 对照：Spring 的 `@GetMapping` 收到 POST 也返回 405，行为一致。

</details>

---

**Q2.** `addr := ":8090"` 里的冒号是什么意思？为什么日志打出来是 `[::]:8090`？

<details>
<summary>答案</summary>

格式是 `[网卡地址]:端口`。

**冒号前留空 = 监听所有网卡**（IPv4 的 `0.0.0.0` + IPv6 的 `::`）。

`ln.Addr()` 返回**实际**绑定的地址——不指定网卡时，Go 用 IPv6 通配地址 `[::]`，它在大多数系统上同时接受 IPv4 连接。

**只想本机访问**（不对外暴露）：

```go
addr := "127.0.0.1:8090"
```

</details>

---

**Q3.** 删掉 `import "net"` 但代码里还在用 `net.Listen`，会怎样？反过来，`net` 留着但代码里不用呢？

<details>
<summary>答案</summary>

**两种情况都是编译错误。**

```bash
# 情况 1：用了但没 import
undefined: net

# 情况 2：import 了但没用
"net" imported and not used
```

**这是和 Java 最大的日常差异之一。** Java 里 unused import 只是 IDE 标灰，编译照过；Go 里直接 build 失败。

好处是 Go 代码里永远不会有残留的无用 import。VS Code 配了 `goimports` 后会**自动增删 import**，所以实际上你不太会碰到这个错误。

</details>

---

**Q4.** 把 `Store.links` 改成 `map[string]*Link`，`Get` 直接返回指针。功能测试还过吗？`-race` 呢？

<details>
<summary>答案</summary>

**功能测试（不加 `-race`）照样过。** 但 `-race` 会报 `DATA RACE`：

```
==================
WARNING: DATA RACE
Write at 0x... by goroutine 12:
  main.(*Store).IncrementHits()
Previous read at 0x... by goroutine 8:
  main.handleRedirect()
```

**原因**：`Get` 返回指针后，调用方在**锁外**读 `link.URL` / `link.Hits`，而 `IncrementHits` 在锁内写 `link.Hits`。

`Get` 里虽然加了 `RLock`，但**锁在返回指针的那一刻就失效了**。

**这正是 M1 存值不存指针的原因。** 这类 bug 靠读代码基本看不出来——`Get` 里明明有锁。

</details>

---

**Q5.** `IncrementHits` 里去掉 `s.links[code] = l` 那一行，哪个测试会失败？失败信息是什么？

<details>
<summary>答案</summary>

两个测试会失败：

**`TestCreateThenRedirect`**：
```
--- FAIL: TestCreateThenRedirect
    Hits = 0, 期望 1（IncrementHits 里漏写 s.links[code] = l？）
```

**`TestConcurrentRedirect`**：
```
--- FAIL: TestConcurrentRedirect
    Hits = 0, 期望 200 —— 计数丢失
```

**原因**：`l, ok := s.links[code]` 取出来的是**值拷贝**，`l.Hits++` 只改了副本。不写回的话，map 里那份永远是 0。

**Java 里不会有这个坑**，因为 `map.get()` 返回对象引用，直接改就改到 map 里了。

</details>

---

**Q6.** 为什么用 302 而不是 301？

<details>
<summary>答案</summary>

**302 是临时跳转，浏览器每次都回来问服务器。**

| | 301 | 302 |
|---|---|---|
| 浏览器缓存 | **永久** | 不缓存 |
| 能否统计访问 | ❌ | ✅ 每次经过服务器 |
| 能否改目标 | ❌ 用户还往老地方跳 | ✅ |

**短链服务必须用 302**，因为：

1. **要统计点击量**——301 之后浏览器直接跳走，服务器根本收不到请求
2. **要能改目标地址**——假设某条链接指向了钓鱼网站，用 301 的话**你改不了**，用户浏览器里缓存的跳转一直生效，只能换新短码

**这是有实际后果的选择，不是风格问题。**

</details>

---

**Q7.** `wg.Add(1)` 为什么必须写在 `go` 之前？

<details>
<summary>答案</summary>

写在 goroutine **内部**的话：

```go
// ❌
for range 200 {
	go func() {
		wg.Add(1)      // 可能还没执行到
		defer wg.Done()
		...
	}()
}
wg.Wait()
```

`wg.Wait()` 在主 goroutine 里执行时，计数可能还是 **0**（因为所有 `Add` 都还没跑）——于是 `Wait()` **立即返回**。

结果：**测试提前结束**，goroutine 还在后台跑着，断言的数据不完整。

**规则：`Add` 必须在 `go` 之前调用。**

```go
// ✅
wg.Add(1)
go func() {
	defer wg.Done()
	...
}()
```

</details>

---

**Q8.** 为什么 `t.Fatalf` 不能在子 goroutine 里用？

<details>
<summary>答案</summary>

`t.Fatalf` 内部调用 `runtime.Goexit()`，它**只终止当前 goroutine**。

在子 goroutine 里调用时，**测试主 goroutine 不会停**——`wg.Wait()` 可能永远等不到 `Done`（虽然 `defer` 会跑），或者测试以诡异的方式挂起、输出混乱。

**规则：子 goroutine 里一律用 `t.Errorf`。**

```go
go func() {
	defer wg.Done()
	if rec.Code != http.StatusCreated {
		t.Errorf("...")     // ✅
		// t.Fatalf("...")  // ❌
	}
}()
```

</details>

---

**Q9.** `Save(l Link)` 接收值而不是 `*Link`，有什么实际好处？

<details>
<summary>答案</summary>

**天然隔离，不需要防御性拷贝。**

调用方 `store.Save(link)` 时，`link` 被**整个拷贝**一份存进 map。之后调用方再改自己那份 `link`，**完全影响不到存储里的数据**。

```go
link := Link{Code: "abc", URL: "https://a.com", Hits: 0}
store.Save(link)

link.URL = "https://evil.com"   // 改的是自己那份拷贝

got, _ := store.Get("abc")
// got.URL 仍然是 "https://a.com"  ✅
```

Java 里你必须手动做 defensive copy：

```java
map.put(code, new Link(link));   // 不然外面改了链接，map 里也跟着变
```

**这是值语义免费给你的好处。**

</details>

---

**Q10.** 为什么 M1 要把路由抽成 `routes()` 函数，而不是直接写在 `main()` 里？

<details>
<summary>答案</summary>

**因为测试不能调 `main()`**——那会真的去监听 8090 端口，测试之间会互相抢端口，还会残留进程。

抽出来之后，测试可以：

```go
mux := routes()
mux.ServeHTTP(rec, req)    // 直接调，不经过网络、不占端口
```

所以 `TestCreateLink` 等测试才能在 **0.5 秒**内跑完。

**这是 Go 里让 `main` 包可测试的标准做法。** M2 会把路由拆到独立的 `internal/router` 包里，思路是一样的。

</details>

---

## 14. M2 预告

M2 要动刀的地方：

| M1 的问题 | M2 的解法 |
|---|---|
| `var store = NewStore()` 全局变量 | `type App struct{ store *Store }` + 构造注入 |
| `Store` 是具体类型，换 Redis 要改所有 handler | 抽 `Repository` **接口** |
| 6 个文件平铺 `package main` | `cmd/` + `internal/{model,repository,service,handler}` |
| 测试必须手动 `store = NewStore()` 重置 | 每个测试 new 一个自己的 `App` |

**M2 的核心是理解一件事**：

> **Go 的接口定义在使用方（consumer），而不是实现方。**

这和 Java **完全相反**。Java 里：

```java
// repository 包
public interface LinkRepository { ... }
public class MemoryLinkRepository implements LinkRepository { ... }
```

Go 里：

```go
// service 包 —— 接口定义在这里，由【使用方】说了算
type LinkRepository interface {
	Save(Link) error
	Get(string) (Link, bool)
}

type LinkService struct {
	repo LinkRepository   // 依赖接口，不依赖具体实现
}
```

`repository` 包**根本不知道有这个接口存在**，它只是提供了满足方法集的类型。

**为什么这样更好**：接口由使用方定义，就只会包含使用方真正需要的方法——**接口自然就小了**。Java 那种"实现方定义接口"的方式，容易造出 20 个方法的大接口。

---

## 参考

- [`net/http` 文档](https://pkg.go.dev/net/http)
- [`sync` 文档](https://pkg.go.dev/sync)（含 `sync.Map` 适用场景的官方说明）
- [Effective Go](https://go.dev/doc/effective_go)
- [Go 1.22 路由增强](https://go.dev/blog/routing-enhancements)
- [Go 1.22 循环变量修复](https://go.dev/blog/loopvar-preview)
- [Go 内存模型](https://go.dev/ref/mem)
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)

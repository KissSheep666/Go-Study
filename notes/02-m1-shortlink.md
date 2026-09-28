# M1：短链接服务 —— 纯 `net/http` + 内存存储

> 面向有 Java / Spring Boot 经验的开发者。
> 配套代码：`shortlink/`（源码里有逐行 Java 对照注释，本文档给的是精简版 + 讲解）
> 前置阅读：[`01-java-to-go.md`](01-java-to-go.md)

## 目录

- [一、M1 目标与验收标准](#一m1-目标与验收标准)
- [二、项目结构](#二项目结构)
- [三、跑起来](#三跑起来)
- [四、逐文件讲解](#四逐文件讲解)
  - [4.1 go.mod —— 对照 pom.xml](#41-gomod--对照-pomxml)
  - [4.2 link.go —— 数据模型与短码生成](#42-linkgo--数据模型与短码生成)
  - [4.3 store.go —— 并发安全的内存存储](#43-storego--并发安全的内存存储)
  - [4.4 handler.go —— HTTP 处理函数](#44-handlergo--http-处理函数)
  - [4.5 main.go —— 组装与启动](#45-maingo--组装与启动)
  - [4.6 handler_test.go —— 测试](#46-handler_testgo--测试)
- [五、Java ↔ Go 对照总表](#五java--go-对照总表)
- [六、七个关键知识点](#六七个关键知识点)
- [七、踩坑记录](#七踩坑记录)
- [八、自测题](#八自测题)
- [九、M2 预告](#九m2-预告)

---

## 一、M1 目标与验收标准

**一句话**：用**标准库**写出一个能跑的短链接服务，不引任何第三方依赖。

M1 刻意**不分层、不做依赖注入**——全部代码扁平地放在一个 `package main` 里。这是故意的：到 M2 你会**真切感受到扁平代码的痛**，然后才明白分层和依赖注入为什么存在。

### 验收标准

| # | 标准 | 状态 |
|---|---|---|
| 1 | `POST /api/links` 接收 `{"url":"..."}`，返回 `201` + 短码 | ✅ |
| 2 | `GET /{code}` 返回 `302` + `Location` 头 | ✅ |
| 3 | `GET /health` 返回服务状态 | ✅ |
| 4 | 非法 URL / 非法 JSON 返回 `400` | ✅ |
| 5 | 不存在的短码返回 `404` | ✅ |
| 6 | 访问计数正确累加 | ✅ |
| 7 | **并发安全**，`-race` 无警告 | ✅ |
| 8 | 有测试覆盖，`go test` 通过 | ✅ |

### 这一阶段会碰到的 Go 概念

`package` / `import` / `func main` / `struct` + 字段导出规则 / struct tag / `map` / `sync.RWMutex` / `defer` / 多返回值 `(T, bool)` / 值语义与拷贝 / `http.Handler` 接口 / 匿名函数 / `:=` / 表驱动测试 / `httptest` / goroutine + `WaitGroup`

---

## 二、项目结构

```
shortlink/
├── go.mod              # ≈ pom.xml
├── main.go             # 入口 + 路由注册 + 全局 store
├── link.go             # Link 模型 + 短码生成 + URL 校验
├── store.go            # 内存存储（map + RWMutex）
├── handler.go          # 三个 HTTP 处理函数 + 响应辅助函数
└── handler_test.go     # 测试
```

全部 `package main`。**6 个文件平铺在一个包里**——这就是 M1 要展示的"原始状态"。

对照一下 M2 之后的样子：

```
shortlink/
├── cmd/api/main.go
└── internal/
    ├── model/       # Link
    ├── repository/  # Store 接口 + 内存实现
    ├── service/     # 业务逻辑
    └── handler/     # HTTP 层
```

---

## 三、跑起来

```bash
cd shortlink

# 格式化（-l 只列出不合规的文件，-w 直接改写）
gofmt -l -w .

# 编译
go build -o shortlink.exe .

# 跑测试
go test -v .

# 带竞态检测跑测试（Windows 需要 CGO_ENABLED=1 + gcc，见环境文档）
CGO_ENABLED=1 go test -race -v .

# 启动服务（端口 8090，因为 8080 被本机 Java 项目占了）
./shortlink.exe
```

### 手工验证

```bash
# 健康检查
curl -i http://127.0.0.1:8090/health

# 创建短链
curl -i -X POST http://127.0.0.1:8090/api/links \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://go.dev/ref/spec"}'
# → 201 {"code":"aB3xK9","url":"https://go.dev/ref/spec","created_at":"...","hits":0}

# 跳转（-i 看响应头，-L 会跟随跳转，这里不要加）
curl -i http://127.0.0.1:8090/aB3xK9
# → 302，Location: https://go.dev/ref/spec

# 不存在的短码
curl -i http://127.0.0.1:8090/nope
# → 404 {"error":"link not found"}

# 非法 URL
curl -i -X POST http://127.0.0.1:8090/api/links -d '{"url":"example.com"}'
# → 400 {"error":"url must start with http:// or https://"}
```

### 实测结果

```
$ CGO_ENABLED=1 go test -race -count=1 .
ok  	shortlink	1.591s        ← 无 DATA RACE 警告

$ curl -s -i http://127.0.0.1:8090/health
HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8
{"links":0,"status":"ok"}
```

---

## 四、逐文件讲解

### 4.1 `go.mod` —— 对照 pom.xml

```
module shortlink

go 1.27.1
```

就这三行。对照 Maven：

| Maven | Go | 说明 |
|---|---|---|
| `pom.xml` | `go.mod` | |
| `groupId:artifactId:version` | `module shortlink` | 本地练手项目用短名字即可；发布才写成 `github.com/你/shortlink` |
| `<maven.compiler.source>` | `go 1.27.1` | 语言版本 |
| 父子 pom、`<dependencyManagement>` | **不存在** | Go 的依赖是**扁平**的，所有依赖写在一个 `require` 块里 |
| 无对应物 | `go.sum` | 依赖校验和，**必须提交**（≈ `package-lock.json`） |

**M1 零第三方依赖**，所以没有 `require` 块，也没有 `go.sum`。

**产物对比**：

```
shortlink.exe    8.3 MB
```

8.3 MB 里包含了 HTTP 服务器 + TLS + 运行时 + GC。对比一个 hello world 的 Spring Boot fat jar 约 20 MB，**还要外加 200 MB 的 JRE**。

---

### 4.2 `link.go` —— 数据模型与短码生成

```go
package main

import (
	"errors"
	"math/rand/v2"
	"net/url"
	"time"
)

type Link struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
	Hits      int64     `json:"hits"`
}

const (
	CodeLen      = 6
	CodeAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

var ErrInvalidURL = errors.New("url must start with http:// or https://")

func generateCode() string {
	b := make([]byte, CodeLen)
	for i := range b {
		b[i] = CodeAlphabet[rand.IntN(len(CodeAlphabet))]
	}
	return string(b)
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidURL
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrInvalidURL
	}
	if u.Host == "" {
		return ErrInvalidURL
	}
	return nil
}
```

#### 讲解

**① `struct` 没有 getter/setter，字段直接导出**

`Code` 大写 = 包外可见；如果写成 `code` 就是包内私有。

Java 开发者第一反应是"这破坏了封装"。但 Go 的哲学是：**封装靠包（package）而不是靠类**。同一个包内互相信任，跨包才需要边界。而且 Go 有 `internal/` 目录做编译期强制的模块边界——比 Java 的 `private` 颗粒度更粗但更难绕过。

**② `json` tag 不是注解**

```go
Code string `json:"code"`
```

反引号里的是 **struct tag**，本质就是附在字段上的一段字符串。**标准库的 `encoding/json` 通过反射读取它**——没有任何注解处理器、没有编译期代码生成。

Java 对照：Jackson 的 `@JsonProperty("code")`。区别是注解由框架定义和扫描，tag 只是一段约定格式的文本，谁想解析都行。

**③ `math/rand/v2`（Go 1.22+）**

- v2 的全局函数**自动随机播种**，不再需要 `rand.Seed(time.Now().UnixNano())`——那句已经是废弃写法
- v2 的顶层函数**并发安全**，多 goroutine 同时调没问题
- Java 对照：`ThreadLocalRandom.current().nextInt(n)`

**④ `var ErrInvalidURL = errors.New(...)`**

Java 对照：自定义异常类。Go 里错误是**值**不是类型，所以：

- 命名以 `Err` 开头（导出）
- 用 `errors.New` 创建
- 调用方用 `errors.Is(err, ErrInvalidURL)` 判断，而不是 `catch`

**⑤ 短码生成的问题（M4 会改）**

随机生成**有碰撞可能**：两条不同长链接可能抽到同一短码，那时 `Save` 会**静默覆盖**前一条。

62^6 ≈ 568 亿，本地测试基本撞不上，但真实流量下这是必然事件（生日悖论）。M4 会换成"**自增 ID + Base62 编码**"，从根上杜绝碰撞——顺便练原子操作。

---

### 4.3 `store.go` —— 并发安全的内存存储

```go
package main

import "sync"

type Store struct {
	mu    sync.RWMutex
	links map[string]Link
}

func NewStore() *Store {
	return &Store{links: make(map[string]Link)}
}

func (s *Store) Save(l Link) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links[l.Code] = l
}

func (s *Store) Get(code string) (Link, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.links[code]
	return l, ok
}

func (s *Store) IncrementHits(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[code]
	if !ok {
		return
	}
	l.Hits++
	s.links[code] = l // ← 必须写回！
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.links)
}
```

#### 讲解

**① Go 的 map 并发写会直接杀进程**

| | Java `HashMap` | Go `map` |
|---|---|---|
| 并发写后果 | 数据错乱、可能死循环 | **`fatal error: concurrent map writes`** |
| 能否恢复 | 一般能 | **不能**，runtime 抛的，`recover` 也救不回来 |

Go 的 runtime 有并发写检测，一旦发现直接终止整个进程。所以**只要有多个 goroutine 碰同一个 map，就必须加锁**。

**② 为什么不用 `sync.Map`（≈ `ConcurrentHashMap`）**

Go 官方的说法是：`sync.Map` 只适合两种场景——key 基本不变、或者多个 goroutine 读写**不相交**的 key 集合。其他情况用**普通 map + Mutex 更快也更清晰**。

这点和 Java 直觉相反：Java 里 `ConcurrentHashMap` 几乎是默认选择，Go 里 `sync.Map` 是特例工具。

**③ 存值不存指针——这是 M1 最重要的设计决策**

```go
links map[string]Link    // ✅ 存值
// links map[string]*Link  ❌ 存指针
```

如果存 `*Link`：

```go
link, ok := store.Get(code)   // 返回指针
http.Redirect(w, r, link.URL, http.StatusFound)  // 读 link.URL —— 【没加锁】
store.IncrementHits(code)     // 另一个 goroutine 里写 link.Hits
```

`Get` 里确实加了锁，但**锁在返回指针的那一刻就失效了**——调用方拿着指针在锁外读，别的 goroutine 在锁内写。这是真正的数据竞争，`-race` 会抓出来，而**你看着代码会觉得"锁明明加了"**。

存值的话，`Get` 返回**拷贝**，任何人拿到的都是自己的一份，共享可变状态从根上消失。

> 这背后是 Go 的核心哲学：**能不共享就不共享**。这也是 `-race` 在本项目里必须跑起来的原因——这类 bug 靠肉眼看代码基本看不出来。

**④ `IncrementHits` 里最容易漏的一行**

```go
l, ok := s.links[code]   // l 是【拷贝】
l.Hits++
s.links[code] = l        // ← 不写回，计数永远是 0
```

Java 里 `map.get()` 拿到的是**对象引用**，改它就直接改到 map 里的对象了。所以 **Java 开发者 100% 会漏这一行**。

**⑤ `mu` 零值可用**

```go
var mu sync.RWMutex   // 直接就能用，不需要 new，不需要构造函数
```

这就是 Go 的"零值可用"原则。标准库的 `sync.Mutex`、`sync.WaitGroup`、`bytes.Buffer` 都做到了。你自己设计类型时也应该追求这点。

**⑥ `defer` 加锁**

```go
s.mu.Lock()
defer s.mu.Unlock()
```

加锁后**立刻**写 `defer Unlock`，无论函数从哪个分支返回、甚至中途 panic，锁都会释放。Java 对照：`try-finally` 或 `try-with-resources`。

**⑦ `(Link, bool)` 的 "comma ok" 惯用法**

Java 里你会返回 `Optional<Link>` 或 `null`。Go 里：

- 返回 `nil` 不行——`Link` 是值类型，没有 `nil`
- 返回 `*Link` 又要处理 nil 指针
- 所以多返回一个 `bool`

调用方：

```go
link, ok := store.Get("abc123")
if !ok { /* 不存在 */ }
```

---

### 4.4 `handler.go` —— HTTP 处理函数

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type createLinkRequest struct {
	URL string `json:"url"`
}

func handleCreateLink(w http.ResponseWriter, r *http.Request) {
	var req createLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := validateURL(req.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	link := Link{
		Code:      generateCode(),
		URL:       req.URL,
		CreatedAt: time.Now(),
		Hits:      0,
	}
	store.Save(link)

	writeJSON(w, http.StatusCreated, link)
}

func handleRedirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")

	link, ok := store.Get(code)
	if !ok {
		writeError(w, http.StatusNotFound, "link not found")
		return
	}

	store.IncrementHits(code)
	http.Redirect(w, r, link.URL, http.StatusFound)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"links":  store.Len(),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```

#### 讲解

**① 处理函数的签名是固定的**

```go
func(w http.ResponseWriter, r *http.Request)
```

- **没有返回值**——响应通过 `w` 写出去，不像 Spring 那样 `return` 一个对象让框架序列化
- `w` 在前，`r` 在后（`http.Handler` 接口规定的顺序）
- 函数名小写——它不是对外 API，只是包内处理函数

**② `r.PathValue("code")` 是 Go 1.22 的新能力**

路由注册时写 `"GET /{code}"`，`{code}` 就是占位符名。

Java 对照：`@PathVariable String code`。

1.22 之前没这功能，大家要么引 `gorilla/mux`，要么自己从 `r.URL.Path` 切字符串。

**③ 302 还是 301？这是个有后果的选择**

| | 301 Moved Permanently | 302 Found |
|---|---|---|
| 浏览器行为 | **永久缓存**跳转 | 每次都回来问服务器 |
| 影响 | 再也统计不到访问；改了目标用户还往老地方跳（缓存清不掉） | 每次都能统计，随时能改目标 |
| 短链服务 | ❌ 不用 | ✅ **标准选择** |

**④ JSON 解码的默认行为**

`json.Decode` **默认忽略未知字段**，也**不会因为字段缺失报错**——缺失的字段保持零值。

Java 里 Jackson 默认会在字段缺失时也可能报错，很多人习惯配 `FAIL_ON_UNKNOWN_PROPERTIES`。Go 反过来：想严格得显式开 `DisallowUnknownFields()`。

测试用例 `"未知字段（Go 默认忽略）"` 就在验证这个行为。

**⑤ 响应一旦开始写就不能回头**

```go
w.Header().Set(...)   // 先设 header
w.WriteHeader(status) // 再写状态码 —— 这一调用之后 header 就【锁定】了
json.NewEncoder(w).Encode(v)
```

顺序反了的话，`Set` 会被忽略，Go 还会打印 `superfluous response.WriteHeader call` 警告。

推论：**错误处理必须前置**。走到 `Encode` 之后才失败，你已经没法把状态码改成 500 了，只能记日志。这跟 Spring 里"随时 `throw` 让 `@ControllerAdvice` 兜底"是完全不同的思维方式。

**⑥ `json.NewEncoder(w).Encode(v)` 直接流式写**

不经过中间的 `[]byte`，比 `json.Marshal` 再 `w.Write` 少一次内存分配。

**⑦ `_ *http.Request`**

未使用的**参数**是合法的，显式写 `_` 表达"我知道有它，我不用"。

注意不对称：未使用的**局部变量**是**编译错误**，未使用的**参数**不是。这个不对称经常让新手困惑。

**⑧ 没有 AOP，横切逻辑就是普通函数**

`writeJSON` / `writeError` 相当于 Spring 里统一封装 `ResponseEntity` 的那层。Go 里没有 AOP，**谁需要谁调用**。显式、啰嗦，但读代码时一眼能看到发生了什么。

---

### 4.5 `main.go` —— 组装与启动

```go
package main

import (
	"log"
	"net"
	"net/http"
)

var store = NewStore() // ⚠️ M1 故意这么写，M2 会改掉

func main() {
	addr := ":8090"

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("shortlink listening on %s", ln.Addr())

	if err := http.Serve(ln, routes()); err != nil {
		log.Fatal(err)
	}
}

func routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /api/links", handleCreateLink)
	mux.HandleFunc("GET /{code}", handleRedirect)
	return mux
}
```

#### 讲解

**① 那个全局 `store` 是故意留的疤**

```go
var store = NewStore() // ⚠️
```

它有三个真实问题：

1. **隐式依赖**——看 `handleCreateLink` 的签名，完全看不出它依赖 `store`。Java 里你至少能在字段上看到 `@Autowired private Store store;`
2. **无法隔离测试**——所有测试共享同一份数据，测试之间互相污染。`handler_test.go` 里每个用例都得手动 `store = NewStore()` 重置，这就是坏味道
3. **无法替换实现**——想从内存换成 Redis，得改所有 handler

M2 会改成显式依赖注入：

```go
type App struct{ store *Store }
func (a *App) handleCreateLink(w http.ResponseWriter, r *http.Request) { ... }
```

到那时你会发现：**Go 的"显式优于隐式"不是口号，是被这种痛逼出来的。**

**② 为什么先 `net.Listen` 再 `http.Serve`，而不是 `http.ListenAndServe`**

这不是风格偏好，是**实际踩到的 bug**：

`ListenAndServe` 把"绑定端口"和"开始服务"揉成一个调用，出错返回时分不清是哪一步失败。结果是日志打出了 `listening on :8090`，但**端口根本没绑上、进程立刻退了**——日志在骗人。

拆成两步后：

- 绑定成功才打日志，日志不骗人
- 拿到 `ln` 之后才有机会做优雅关闭（M6 的 `srv.Shutdown`）

Java 对照：这一步相当于 Tomcat Connector 绑定端口，Spring Boot 把它藏在自动配置里，Go 让你显式写出来。

**③ 路由优先级（Go 1.22+）**

```
GET /health     ← 更具体
GET /{code}     ← 更宽泛
```

`/health` 不会被 `/{code}` 抢走——**最具体的模式优先**。而 `/api/links` 有两段，`/{code}` 只匹配单段路径，也不冲突。

测试 `TestHealth` 就是在验证这个优先级。

**④ 为什么把路由抽成 `routes()` 函数**

测试不能调 `main()`（那会真的监听端口）。抽出来测试才能拿到同一份路由表。这是 Go 里让 `main` 包可测试的标准做法。

**⑤ 不需要线程池**

```go
http.Serve(ln, mux)   // 阻塞主 goroutine
```

`net/http` 内部**为每个请求起一个 goroutine**。Java 对照：`SpringApplication.run()` + Tomcat 线程池。

区别是 goroutine 初始栈只有 **2KB**，可以轻松开十万个；而 Java 线程每个约 1MB，必须用线程池限制数量。**这是 Go 在 IO 密集型服务上的核心优势。**

---

### 4.6 `handler_test.go` —— 测试

```go
func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
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
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateURL(%q) 返回 err=%v, 期望 wantErr=%v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestCreateThenRedirect(t *testing.T) {
	store = NewStore()
	mux := routes()

	// 创建
	createReq := httptest.NewRequest(http.MethodPost, "/api/links",
		strings.NewReader(`{"url":"https://go.dev/ref/spec"}`))
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("创建失败，状态码 = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	var created Link
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}

	// 跳转
	redirectReq := httptest.NewRequest(http.MethodGet, "/"+created.Code, nil)
	redirectRec := httptest.NewRecorder()
	mux.ServeHTTP(redirectRec, redirectReq)

	if redirectRec.Code != http.StatusFound {
		t.Errorf("状态码 = %d, 期望 302", redirectRec.Code)
	}
	if got := redirectRec.Header().Get("Location"); got != "https://go.dev/ref/spec" {
		t.Errorf("Location = %q, 期望 %q", got, "https://go.dev/ref/spec")
	}

	// 计数 +1
	link, _ := store.Get(created.Code)
	if link.Hits != 1 {
		t.Errorf("Hits = %d, 期望 1（IncrementHits 里漏写 s.links[code] = l？）", link.Hits)
	}
}

func TestConcurrentCreate(t *testing.T) {
	store = NewStore()
	mux := routes()

	const n = 200
	var wg sync.WaitGroup

	for range n {
		wg.Add(1) // ← 必须在 go 之前
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/links",
				strings.NewReader(`{"url":"https://go.dev"}`))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Errorf("并发创建失败，状态码 = %d", rec.Code) // ← 不能用 Fatalf
			}
		}()
	}

	wg.Wait()

	if got := store.Len(); got != n {
		t.Errorf("并发创建后 Len() = %d, 期望 %d —— 有写入丢失，检查锁", got, n)
	}
}
```

#### 讲解

**① 表驱动测试是 Go 的标准写法**

Java 对照：JUnit 5 的 `@ParameterizedTest` + `@MethodSource`。

Go 里**不需要任何注解和框架**——就是普通的匿名 struct 切片 + `for` 循环。为什么用匿名 struct？因为它只用这一次，起个名字反而啰嗦。

**② 测试文件和源码放同一个目录、同一个包**

Java 是 `src/main/java` 和 `src/test/java` 两棵树；Go 是 `xxx.go` 和 `xxx_test.go` **并排放**。

- 好处：测试能访问包内私有成员（比如这里的 `store`、`generateCode`、`validateURL`）
- `_test.go` 不会进编译产物，不用担心体积

**③ `httptest` 替代 MockMvc**

| Java | Go |
|---|---|
| `MockMvc.perform(post("/api/links"))` | `httptest.NewRequest(...)` |
| `MockHttpServletResponse` | `httptest.NewRecorder()` |
| `mvc.perform(...)` | `routes().ServeHTTP(rec, req)` |

**不启服务器**，直接调路由——所以测试跑得飞快（整个套件 0.5 秒）。

**④ `strings.NewReader(body)` 当请求体**

`strings.NewReader` 把字符串包成 `io.Reader`，而 `http.Request.Body` 就是 `io.Reader`。**Go 的接口让"字符串"变成了"可读流"。** Java 里你得用 `new ByteArrayInputStream(s.getBytes())`。

**⑤ goroutine 里绝对不能用 `t.Fatalf`**

`t.Fatalf` 内部调 `runtime.Goexit()`，只终止**当前 goroutine**。在子 goroutine 里调用，测试主 goroutine 不会停，会得到诡异的输出和挂起的测试。

**规则：子 goroutine 里一律用 `t.Errorf`。**

**⑥ `wg.Add(1)` 必须在 `go` 之前**

写在 goroutine 内部的话，可能出现 `wg.Wait()` 已经返回、某个 goroutine 才刚开始执行 `Add` 的情况——经典的竞态 bug。

**⑦ 并发测试的真正价值**

`TestConcurrentCreate` / `TestConcurrentRedirect` **不加 `-race` 也能过**。它们的价值在于：

```bash
CGO_ENABLED=1 go test -race -count=1 .
```

加了 `-race` 之后，它会检查 `Store` 的加锁是否真的正确。**如果把 `mu` 去掉，或者把 `links` 改成 `map[string]*Link`，这个测试会立刻报 `concurrent map writes` 或 `DATA RACE`。**

这就是为什么 M1 要专门写并发测试：让你亲眼看到"锁不是装饰品"，以及 `-race` 到底在防什么。

---

## 五、Java ↔ Go 对照总表

| 概念 | Java / Spring Boot | Go |
|---|---|---|
| 构建文件 | `pom.xml` | `go.mod`（3 行） |
| 包声明 | `package com.example.shortlink;` | `package main` |
| 导入 | `import java.util.List;` | `import "net/http"`（**不用即编译错误**） |
| 入口 | `public static void main(String[])` | `func main()` |
| 数据模型 | `class Link` + Lombok `@Data` | `type Link struct`（**无需 getter/setter**） |
| 字段序列化 | Jackson `@JsonProperty` | struct tag `` `json:"code"` `` |
| 常量 | `static final int N = 6;` | `const N = 6` |
| 自定义异常 | `class MyException extends Exception` | `var ErrX = errors.New("...")` |
| 抛异常 | `throw new XException()` | `return fmt.Errorf("ctx: %w", err)` |
| 捕获 | `try / catch / finally` | `if err != nil` / `defer` |
| 判空 | `Optional<T>` / `null` | `(T, bool)` comma-ok |
| 控制器 | `@RestController` + `@PostMapping` | `mux.HandleFunc("POST /path", fn)` |
| 路径参数 | `@PathVariable` | `r.PathValue("code")` |
| 请求体 | `@RequestBody DTO` | `json.NewDecoder(r.Body).Decode(&v)` |
| 响应 | `return ResponseEntity<T>` | `w.WriteHeader()` + `json.NewEncoder(w).Encode(v)` |
| 依赖注入 | `@Autowired` / IoC 容器 | **手写** `NewXxx(dep)` 构造注入 |
| 并发容器 | `ConcurrentHashMap` | 普通 `map` + `sync.RWMutex`（**`sync.Map` 是特例工具**） |
| 读写锁 | `ReentrantReadWriteLock` | `sync.RWMutex`（**零值可用**） |
| 发放任务 | `executor.submit(runnable)` | `go f()`（**无需线程池**） |
| 等待完成 | `CountDownLatch` | `sync.WaitGroup` |
| HTTP 测试 | `MockMvc` + `MockHttpServletResponse` | `httptest.NewRequest` + `NewRecorder` |
| 参数化测试 | `@ParameterizedTest` + `@MethodSource` | 匿名 struct 切片 + `for` |
| 测试代码位置 | `src/test/java` | 同目录 `xxx_test.go` |
| 横切逻辑 | AOP / `@ControllerAdvice` | 普通函数，谁需要谁调 |
| 产物 | `app.jar` + JRE（~200MB） | **单个静态可执行文件**（8.3MB） |

---

## 六、七个关键知识点

### 1. 字段导出靠首字母大小写

```go
type Link struct {
	Code string // 导出，包外可见 ≈ public
	url  string // 不导出，包内私有 ≈ package-private
}
```

没有 `protected`。跨模块的"受保护"靠 `internal/` 目录（M2 会用到）。

**封装靠包而不是靠类**——这是和 Java 最根本的观念差异。

### 2. 值语义贯穿一切

```go
l, ok := s.links[code]   // l 是拷贝
l.Hits++
s.links[code] = l        // 不写回 = 白改
```

`map` 里存值、取出来是拷贝、必须写回。Java 开发者必踩。

而正因为是值语义，`Get` 返回拷贝就**天然消除了共享可变状态**——这是 Go 里防数据竞争最有力的手段。

### 3. Go 的 map 不是并发安全的

并发写直接 `fatal error: concurrent map writes`，**进程立即死亡**，`recover` 救不回来。

比 Java 的 `HashMap` 严重得多——Java 只是数据错乱，Go 是当场暴毙。

### 4. `http.Handler` 就一个方法

```go
type Handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type HandlerFunc func(w http.ResponseWriter, r *http.Request)

// 给函数类型加方法 —— 普通函数就"变成"了接口
func (f HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f(w, r)
}
```

这 10 行是标准库 `net/http/server.go` 里的真实代码，也是 **M3 中间件的全部基础**。中间件的签名就是：

```go
func Logging(next http.Handler) http.Handler
```

接收一个 Handler，返回一个 Handler。

### 5. `if err := f(); err != nil` 是 Go 的呼吸方式

```go
if err := validateURL(req.URL); err != nil {
	writeError(w, http.StatusBadRequest, err.Error())
	return
}
```

- 声明和判空压成一行
- `err` 作用域**只在这个 if 内**，不污染外层
- 你会写几千遍这个模式

### 6. 响应一旦开始写就不能回头

```go
w.Header().Set(...)    // 先 header
w.WriteHeader(status)  // 再状态码 —— 之后 header 锁定
json.NewEncoder(w).Encode(v)
```

**推论**：错误处理必须前置——把所有可能失败的事做完，最后才一次性写响应。

这和 Spring 里"随时 `throw`，`@ControllerAdvice` 兜底"是完全不同的思维方式。

### 7. 不需要线程池

`net/http` 为每个请求起一个 goroutine，初始栈 **2KB**。

| | Java 线程 | Go goroutine |
|---|---|---|
| 初始栈 | ~1 MB | **2 KB** |
| 数量级 | 数千 | **数十万** |
| 调度 | OS 内核 | runtime（GMP，M:N） |
| 池化 | 必需 | **不需要** |

这是 Go 在 IO 密集型服务上的核心优势。

---

## 七、踩坑记录

### 1. 端口 8080 被占

本机 8080 / 8081 / 8000 / 5173 都被其他项目占用（8080 是「智能实验室预约与设备管理系统」的 Java 服务）。

M1 改用 **8090**。硬编码端口会撞车——这正是 M6 要引入配置管理（环境变量 / 命令行参数）的原因。

### 2. `http.ListenAndServe` 会打出骗人的日志

**现象**：日志显示 `listening on :8080`，但请求全返回 500——那些 500 其实是**另一个进程**（Java 项目）返回的，我们的进程早就因为绑定失败退出了。

**原因**：`ListenAndServe` 把"绑定端口"和"开始服务"揉成一个调用，出错时无法区分。

**修复**：

```go
ln, err := net.Listen("tcp", addr)   // 先显式绑定
if err != nil {
	log.Fatal(err)
}
log.Printf("listening on %s", ln.Addr())   // 绑定成功才打日志
http.Serve(ln, mux)                         // 再开始服务
```

**附加收益**：拿到 `ln` 之后才能做优雅关闭（M6）。

### 3. Windows 上 `-race` 需要 cgo + gcc，且 gcc 必须在 PATH 里

```bash
# ❌ 只加 go 到 PATH
CGO_ENABLED=1 go test -race .   # → FAIL [build failed]

# ✅ 必须同时有 gcc
export PATH="/d/Dev/mingw64/bin:/d/Dev/go/bin:$PATH"
CGO_ENABLED=1 go test -race .   # → ok  shortlink  1.591s
```

**注意**：全局保持 `CGO_ENABLED=0`，让 `go build` 继续产出**纯 Go 静态单文件**（M6 打 Docker 镜像靠它做到 ~10MB）。只在测试时开 cgo。

### 4. 空 `.go` 文件不是合法 Go

一个 0 字节的 `test.go` 会报错——缺 `package` 声明。

而且**文件必须属于某个 module**（所在目录或上级有 `go.mod`），否则 gopls 会降级工作，补全/跳转/格式化都打折扣。

### 5. `req` 忘记声明

```go
// ❌ 编译错误：undefined: req
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {

// ✅
var req createLinkRequest
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
```

用 `:=` 一步到位看着更短，但后面还要用 `req.URL`，所以必须提前声明。

---

## 八、自测题

先自己答，再看答案。

**Q1.** 把 `mux.HandleFunc("GET /health", ...)` 改成 `mux.HandleFunc("/health", ...)`（去掉 `GET `），`POST /health` 会返回什么？

**Q2.** `addr := ":8090"` 里的冒号是什么意思？为什么日志打出来是 `[::]:8090`？

**Q3.** 删掉 `import "net"` 但代码里还在用 `net.Listen`，会怎样？反过来，`net` 留着但代码里不用呢？

**Q4.** 把 `Store.links` 改成 `map[string]*Link`，`Get` 直接返回指针。功能测试还过吗？`-race` 呢？

**Q5.** `IncrementHits` 里去掉 `s.links[code] = l` 那一行，哪个测试会失败？失败信息是什么？

**Q6.** 为什么用 302 而不是 301？

<details>
<summary>答案</summary>

**A1.** 返回 `200`。因为 `/health` 不限定方法，`POST` 也能匹配。原来的 `"GET /health"` 限定方法后，`POST /health` 返回 **405 Method Not Allowed**。

**A2.** 冒号前是**网卡地址**，留空表示监听所有网卡（0.0.0.0 / ::）。冒号后是端口。`ln.Addr()` 返回实际地址——不指定网卡时内存里是 `[::]`（IPv6 通配，同时接受 IPv4），所以打印出来是 `[::]:8090`。

**A3.** 前者是**编译错误** `undefined: net`。后者也是**编译错误** `"net" imported and not used`。Go 里 import 了不用就是错误——这是和 Java 最大的日常差异之一。

**A4.** **功能测试（不加 `-race`）照样过。** 但 `-race` 会报 `DATA RACE`——因为 `Get` 返回指针后，调用方在锁外读 `link.URL`/`link.Hits`，而 `IncrementHits` 在锁内写 `link.Hits`。**这正是 M1 存值不存指针的原因。**

**A5.** `TestCreateThenRedirect` 和 `TestConcurrentRedirect` 会失败，报 `Hits = 0, 期望 1`（或 `期望 200`）。因为 `l` 是从 map 拷贝出来的副本，改它不影响存储。

**A6.** 302 是临时跳转，浏览器**每次都回来问服务器**——这样才能统计每次访问，也才能随时修改目标地址。301 会被浏览器永久缓存，之后访问根本不到服务器，统计和改址都做不到了。

</details>

---

## 九、M2 预告

M2 要解决 M1 留下的三个问题：

| M1 的问题 | M2 的解法 |
|---|---|
| 全局 `var store`，依赖隐式 | `type App struct{ store *Store }` + 显式构造注入 |
| `Store` 是具体类型，换 Redis 要改所有 handler | 抽 `Repository` **接口**，定义在**使用方**（service 包） |
| 6 个文件平铺在 `package main` | `cmd/` + `internal/{model,repository,service,handler}` |

会学到的核心概念：

- **接口定义在使用方（consumer）而不是实现方**——和 Java 相反
- `internal/` 目录：编译器强制的模块边界
- 为什么 Go 不需要 IoC 容器，手写 `NewXxx(dep)` 反而更好
- 包的组织原则：按**职责**分而不是按**类型**分

---

## 参考

- [Effective Go](https://go.dev/doc/effective_go)
- [`net/http` 文档](https://pkg.go.dev/net/http)
- [`sync` 文档](https://pkg.go.dev/sync)（含 `sync.Map` 适用场景的官方说明）
- [Go 1.22 路由增强](https://go.dev/blog/routing-enhancements)
- [Go 内存模型](https://go.dev/ref/mem)

# Java → Go 前置知识对照

> 面向有 Java 后端经验的开发者。只讲**和 Java 不一样的地方**，以及**会踩坑的地方**。
> 目标版本：Go 1.22+（本项目用最新稳定版）

## 目录

- [一、七条思维转变（最重要）](#一七条思维转变最重要)
- [二、语法速查对照表](#二语法速查对照表)
- [三、并发模型对照](#三并发模型对照)
- [四、工程化对照（Maven/Gradle → go mod）](#四工程化对照mavengradle--go-mod)
- [五、标准库 vs Java 类库](#五标准库-vs-java-类库)
- [六、Web 服务最小对照](#六web-服务最小对照)
- [七、Java 开发者 Top 12 踩坑清单](#七java-开发者-top-12-踩坑清单)

---

## 一、七条思维转变（最重要）

语法一周就能记住，思维转变要三个月。这七条是核心。

### 1. 没有类，只有 struct + 方法

```go
// Java
// public class User {
//     private String name;
//     public String getName() { return name; }
//     public void rename(String n) { this.name = n; }
// }

// Go
type User struct {
    Name string // 大写 = 导出（public）；小写 = 包内私有
    age  int    // 包外看不见，没有 protected 这种东西
}

func (u User) Display() string { return u.Name }      // 值接收者：拿到的是【拷贝】
func (u *User) Rename(n string) { u.Name = n }        // 指针接收者：能改原对象
```

关键差异：

- 访问控制靠**首字母大小写**，没有 `public/private/protected`。跨模块的"受保护"靠 `internal/` 目录。
- 方法定义在 struct **外面**，接收者写在 `func` 和函数名之间。
- 没有 `this`，接收者名字自己取（惯例用短名 `u`，**不要**用 `this`/`self`）。
- **值接收者 = 拷贝**。这是 Java 人最大的坑 —— Java 传对象永远是引用，Go 传 struct 默认是深拷贝。
- 惯例：同一个类型的方法集要么全用值接收者，要么全用指针接收者，别混。

### 2. 组合优于继承 —— Go 根本没有继承

```go
type Base struct{ ID int64 }

func (b Base) Describe() string { return "base" }

type Order struct {
    Base           // 嵌入（embedding），是【字段提升】的语法糖，不是继承
    Amount int64
}

o := Order{Base: Base{ID: 1}}
o.Describe()  // 能调，但底层是 o.Base.Describe()，编译器帮你转发
o.ID          // 同理
```

- 没有 `extends`、没有 `@Override`、没有 `super`、没有抽象类。
- 嵌入**不产生多态**。`Order` 不是 `Base`，不能把 `Order` 赋给 `Base` 类型的变量。
- 想要多态 → 用**接口**。想要复用 → 用**组合 + 接口**，或把函数当参数传。
- 策略模式在 Go 里就是一个函数类型：`type Handler func(x int) error`。

### 3. 接口是隐式实现的（鸭子类型）

```go
type Reader interface {
    Read(p []byte) (n int, err error)
}

// 任何拥有这个方法签名的类型，自动就是 Reader —— 不需要 implements
// 标准库根本不认识你的类型，你也不认识标准库的接口定义
```

跟 Java 相反的两点：

1. **接口定义在使用方（consumer），不是实现方。** Java 里你写 `class UserService implements UserRepo`；Go 里 `UserRepo` 接口应该定义在 `service` 包里，由 `service` 说了算，`repository` 包只管提供方法。
2. **小接口是常态。** `io.Reader`/`io.Writer`/`fmt.Stringer`/`error` 都只有 1 个方法。Go 谚语："接口越大，抽象越弱。"

**接口判空陷阱（必踩）**：

```go
type MyErr struct{}
func (e *MyErr) Error() string { return "boom" }

func bad() error {
    var p *MyErr = nil
    return p          // 返回了一个"装着 nil 指针的 error 接口"
}

if err := bad(); err != nil {
    // 这里会进来！err != nil 为 true！
}
```

接口值 = `(动态类型, 动态值)` 二元组。只有两者**都为 nil**，接口才等于 nil。这里是 `(*MyErr, nil)`，类型不为 nil，所以整体不为 nil。

> 防护：函数里如果可能返回 nil 指针，显式 `return nil` 而不是 return 那个变量。

### 4. 没有异常，只有 error 返回值

```go
f, err := os.Open("config.yaml")
if err != nil {
    return fmt.Errorf("open config: %w", err) // %w = 包装，保留错误链
}
defer f.Close() // 相当于 finally，但注册在调用点，可读性更好
```

| Java | Go |
|---|---|
| `try / catch / finally` | `if err != nil` / `defer` |
| `throw new XException()` | `return fmt.Errorf("ctx: %w", err)` |
| `throws IOException` | 无声明，`error` 就是普通返回值 |
| `e.getMessage()` | `err.Error()` |
| `catch (FileNotFoundException e)` | `errors.Is(err, os.ErrNotExist)` |
| `catch (XException e) { e.getCode() }` | `errors.As(err, &target)` |
| `finally { f.close() }` | `defer f.Close()` |
| `RuntimeException`（免检） | `panic` —— **只用于程序 bug，绝不用于业务错误** |

**panic 不是异常**：

- panic 只能在**同一个 goroutine 内**被 recover 捕获。别的 goroutine panic 了，你的 recover 抓不到，整个进程挂掉。
- 惯例是不让 panic 跨越包边界：包内部 panic，包入口 recover 转成 error。
- 业务错误（用户不存在、余额不足）一律 `error`。
- `defer` 里调用 `recover()` 只在**直接被 defer 的那个函数**里有效，嵌套函数里无效。

**`defer` 三个必须知道的点**：

```go
// 1. 参数在 defer 语句执行时求值，不是函数返回时
i := 1
defer fmt.Println(i) // 打印 1，不是 2
i = 2

// 2. 多个 defer 是栈：后进先出
defer a() // 第 3 个执行
defer b() // 第 2 个执行
defer c() // 第 1 个执行

// 3. 循环里 defer 会累积，直到函数返回才执行 —— 大循环里别这么写
for _, f := range files {
    defer f.Close() // 文件句柄会一直攒着，可能耗尽 fd
}
```

### 5. 值语义 —— Go 里几乎一切都是拷贝

```go
type Point struct{ X, Y int }

p1 := Point{1, 2}
p2 := p1      // 深拷贝整个 struct
p2.X = 99     // p1.X 仍然是 1

s1 := []int{1, 2, 3}
s2 := s1      // 只拷贝 slice header（指针, len, cap），底层数组共享
s2[0] = 99    // s1[0] 也变成 99！
```

需要重建的直觉：

| 类别 | 类型 | 赋值/传参行为 |
|---|---|---|
| 值类型 | `int` `string` `bool` `struct` `array [N]T` | **深拷贝** |
| 引用型 | `slice` `map` `chan` `func` `interface` `*T` | 拷贝"描述符"，底层数据共享 |

推论：

- 想修改原对象 → 用指针 `*T`（或 `map`/`slice` 这类天然共享的）。
- 大 struct 传参想避免拷贝 → 用指针。
- `map` 和 `chan` 本身就是引用，**传 map 不需要传 `*map`**（这是常见误解）。
- 方法用值接收者还是指针接收者，直接决定"能不能改到原对象"。

### 6. 零值可用（zero value is useful）

```go
var s string          // ""
var n int             // 0
var b bool            // false
var p *User           // nil
var sl []int          // nil —— 安全：len=0，可以 range，可以 append
var m map[string]int  // nil —— 可读（返回零值），【写入会 panic】
var mu sync.Mutex     // 可以直接用，无需构造！
var buf bytes.Buffer  // 同上
```

- Go 没有构造函数语法，只有 `NewXxx()` 的**命名约定**。
- 设计良好的类型应该**零值即可用**，无需 `New`。标准库的 `sync.Mutex`、`bytes.Buffer`、`sync.WaitGroup` 都做到了。
- `nil map` 写入 panic，必须 `make(map[string]int)` 或字面量 `map[string]int{}`。
- `nil slice` 和空 slice 在 `len`/`range`/`append` 上行为一致，一般不用刻意区分。

### 7. 命名与风格（gofmt 统一，没有配置项）

- 导出 = 大写开头，包内私有 = 小写开头。
- 缩写保持大小写一致：`userID`、`HTTPServer`、`parseURL`（**不是** `userId`、`HttpServer`）。
- 包名短、小写、单数、无下划线：`user`、`http`、`order`（**不是** `userUtils`、`user_service`）。
- 没有 getter/setter 文化。字段需要暴露就**直接导出** `u.Name`；只有需要校验/副作用时才加方法。
- 错误信息：小写开头、不加标点、不重复上下文。`errors.New("user not found")`，而不是 `errors.New("User not found.")`。
- 错误变量命名以 `Err` 开头：`ErrNotFound`、`ErrDuplicate`。
- `gofmt` 是强制的，团队不为此争论 —— 这跟 Java 里 Checkstyle 配置打架是两回事。

---

## 二、语法速查对照表

| 概念 | Java | Go |
|---|---|---|
| 变量声明 | `String s = "a";` | `s := "a"` / `var s string = "a"` |
| 常量 | `static final int N = 3;` | `const N = 3`（**无类型常量**，很灵活） |
| 定长数组 | `int[] a = new int[5];` | `var a [5]int`（**长度是类型的一部分**，很少直接用） |
| 动态数组 | `ArrayList<T>` | `[]T`（slice） |
| Map | `HashMap<K,V>` | `map[K]V` |
| Set | `HashSet<T>` | **没有**，用 `map[T]struct{}` 模拟 |
| 字符串 | `String`，不可变，UTF-16 | `string`，不可变，**UTF-8 字节序列** |
| 字符串长度 | `s.length()` = 字符数 | `len(s)` = **字节数**；`utf8.RuneCountInString(s)` 才是字符数 |
| 遍历字符串 | `for (char c : s)` | `for i, r := range s`（`i` 是字节偏移，`r` 是 rune） |
| 装箱拆箱 | `int` ↔ `Integer` | **没有**，只有放进 `any` 才装箱 |
| 泛型 | `<T extends Comparable<T>>` | `[T comparable]`（1.18+，**用得比 Java 少得多**） |
| 枚举 | `enum Status {...}` | `const` + `iota` + 自定义类型 |
| 继承 | `extends` | 嵌入 struct（语义不同！） |
| 接口 | `implements` 显式声明 | 隐式满足方法集 |
| 异常 | `try/catch` | `error` 返回值 + `panic` |
| 空值 | `null` | `nil`（**只有引用型能是 nil**） |
| 访问控制 | `public/private/protected` | 首字母大小写 + `internal/` |
| 包 | `package com.foo.bar;` | `package bar` + module 路径 |
| 静态方法 | `static` | 包级函数 `user.New()` |
| 函数值 | 需要 lambda / 函数式接口 | 原生 `func` 类型，一等公民 |
| 闭包 | lambda（**捕获需 effectively final**） | 闭包捕获变量本身（可改外部变量） |
| Lambda | `x -> x * 2` | `func(x int) int { return x * 2 }` |
| 三元运算符 | `a ? b : c` | **没有**，写 if |
| 默认参数 | 部分支持 | **没有**，用 Functional Options |
| 方法重载 | 支持 | **没有**，只能换函数名 |
| 变长参数 | `int...` | `...int`（一样） |
| 断言 | JUnit `assertEquals` | `if got != want { t.Errorf(...) }` |

### Go 独有的几个语法点

**① `for` 是唯一的循环关键字**

```go
for i := 0; i < 10; i++ { }      // 经典三段式
for i < 10 { }                    // 相当于 while
for { }                           // 相当于 while(true) —— 没有 while 关键字
for i, v := range slice { }       // 相当于 for-each
for k, v := range m { }           // map
for i, r := range "中文" { }       // i 是字节偏移，r 是 rune
for range 5 { }                   // Go 1.22+：只循环 5 次，不要下标
```

**② `iota` 做枚举**

```go
type Status int

const (
    StatusPending Status = iota // 0
    StatusRunning               // 1
    StatusDone                  // 2
)

// 对标 toString()：实现 Stringer 接口
func (s Status) String() string {
    return [...]string{"pending", "running", "done"}[s]
}
```

**③ 多返回值（这是 `if err != nil` 的来源）**

```go
func div(a, b int) (int, error) {
    if b == 0 {
        return 0, errors.New("division by zero")
    }
    return a / b, nil
}

q, err := div(10, 2)
```

**④ 命名返回值**（少用，但标准库常见）

```go
func parse(s string) (n int, err error) {
    defer func() {
        if err != nil {
            err = fmt.Errorf("parse %q: %w", s, err) // 能改到返回值
        }
    }()
    // ...
    return
}
```

**⑤ Go 里没有的东西清单**

`while` / `do-while` / 三元运算符 / 方法重载 / 默认参数 / 继承 / 抽象类 / 异常 / 注解 / 反射式依赖注入 / `try-with-resources` / Stream API / 包级 `static` 块 / 构造函数 / Set / 装箱

> 每一条都有惯用替代方案，但核心精神是：**Go 用少量语法 + 显式代码，换取可读性和可预测性。**

---

## 三、并发模型对照

### 概念映射

| Java | Go | 备注 |
|---|---|---|
| `new Thread(r).start()` | `go f()` | goroutine 初始栈 **2KB**，可轻松开十万级 |
| `ExecutorService` 线程池 | **不需要** | runtime 自带 M:N 调度（GMP 模型） |
| `Future<T>` / `CompletableFuture` | `chan T` + `select` / `context` | |
| `BlockingQueue<T>` | `chan T`（带缓冲） | `make(chan T, 10)` ≈ `ArrayBlockingQueue(10)` |
| `SynchronousQueue` | `chan T`（无缓冲） | 发送阻塞直到有接收者 = 同步交接 |
| `synchronized` / `ReentrantLock` | `sync.Mutex` | |
| `ReadWriteLock` | `sync.RWMutex` | |
| `AtomicInteger` / `LongAdder` | `atomic.Int64`（1.19+） | 优先用类型化的，别用老的函数式 API |
| `CountDownLatch` | `sync.WaitGroup` | `Add` 必须在 `go` **之前**调用 |
| `CyclicBarrier` | `sync.WaitGroup` 复用 / channel | 没有直接对应 |
| `volatile` | **没有** | 用 channel 或 `atomic`；Go 内存模型靠 happens-before |
| `ThreadLocal` | **没有** | 显式传参，或用 `context.Context` |
| `ConcurrentHashMap` | `sync.Map` | **多数场景应该用普通 `map` + `sync.RWMutex`** |
| `shutdown()` / `awaitTermination()` | `close(ch)` + `context.WithCancel` + `WaitGroup` | |
| 线程池拒绝策略 | `select` + `default` 丢弃，或带缓冲 channel 阻塞 | |

### 核心心法

> **Do not communicate by sharing memory; instead, share memory by communicating.**
> 不要通过共享内存来通信，而要通过通信来共享内存。

Java 里你习惯"加锁保护共享变量"；Go 里首选"把数据通过 channel 交给另一个 goroutine，保证同一时刻只有一个 goroutine 持有它"。

```go
// Java 风格（Go 里也能写，但要小心死锁和忘记 Unlock）
var mu sync.Mutex
var counter int
mu.Lock()
counter++
mu.Unlock()

// Go 风格（CSP）：把写操作串行化到一个 goroutine
ch := make(chan int)
go func() {
    for v := range ch {
        counter += v // 只有这个 goroutine 碰 counter，天然无竞争
    }
}()
ch <- 1
```

两种都对。**判断标准**：共享的是"状态"还是"消息"？状态 → mutex；消息/任务流转 → channel。

### channel 基础

```go
ch := make(chan int)      // 无缓冲：发送方阻塞直到有人接收
ch := make(chan int, 10)  // 有缓冲：满则阻塞发送，空则阻塞接收

ch <- 1                   // 发送
v := <-ch                 // 接收
v, ok := <-ch             // ok=false 表示 channel 已关闭且已排空
close(ch)                 // 【只有发送方】应该 close

for v := range ch { }     // 遍历直到 channel 关闭
```

**close 的铁律**：

- 只有发送方 close（接收方 close 会导致发送方 panic）。
- 向**已关闭**的 channel 发送 → panic。
- 重复 close → panic。
- 从已关闭的 channel 接收 → 立即返回零值，`ok=false`（不会 panic）。
- `nil` channel 上收发 → **永久阻塞**（可以用来在 `select` 里禁用某个 case）。

### select —— 多路复用（Java 没有对应物）

```go
select {
case v := <-ch1:
    fmt.Println("recv", v)
case ch2 <- 42:
    fmt.Println("sent")
case <-time.After(time.Second):
    fmt.Println("timeout")
case <-ctx.Done():
    return ctx.Err()
default:
    // 所有 case 都不就绪时走这里 = 非阻塞
}
```

- `select` 有多个 case 同时就绪时，**随机选一个**（不是按顺序）。
- `select {}`（空 select）永久阻塞 —— 会死锁报错。
- `for { select { ... } }` 是最常见的事件循环骨架。

### context —— 替代 ThreadLocal + 取消传播

```go
// 创建
ctx := context.Background()                                // 根，main/test 用
ctx, cancel := context.WithCancel(ctx)                     // 手动取消
ctx, cancel := context.WithTimeout(ctx, 3*time.Second)     // 超时
ctx, cancel := context.WithDeadline(ctx, t)                // 截止时间
ctx = context.WithValue(ctx, key, val)                     // 传请求域数据

defer cancel() // 【必须调用】，否则父 ctx 不释放，内存泄漏
```

**约定（后端最重要的规范）**：

1. `ctx context.Context` 永远是函数的**第一个参数**。
2. 不要把它塞进 struct 字段（`http.Request` 除外）。
3. `WithValue` 只放**请求域元数据**（traceID、userID、租户ID），**不放可选参数**。
4. key 用**自定义类型**避免冲突：`type ctxKey struct{}`，不要用字符串字面量。
5. 下游函数要**持续检查** `select { case <-ctx.Done(): return ctx.Err() }`，否则取消传不下去。

`http.Request` 自带 context：`r.Context()`；出站请求用 `http.NewRequestWithContext(ctx, ...)`。

### 常见并发坑

1. **循环变量捕获**：Go 1.22 起 `for` 每轮新建变量，已修复。但**显式传参仍是好习惯**，尤其是要兼容老代码：
   ```go
   for _, v := range items {
       v := v // Go 1.22 前必须这么写
       go func() { use(v) }()
   }
   ```
2. **数据竞争**：`go test -race ./...` 必开。CI 里也开。race detector 只能发现**运行时实际发生**的竞争。
3. **goroutine 泄漏**：每个 `go` 出去的 goroutine 都必须有**明确退出路径**（`ctx.Done()` 或 channel close）。往无缓冲 channel 发数据但没人收 = 永久泄漏。
4. **`WaitGroup.Add` 的位置**：必须在 `go` 之前调用，否则 `Wait` 可能在 `Add` 前就跑完了。
5. **`WaitGroup` 不能拷贝**：传参要传 `*sync.WaitGroup`（`go vet` 会抓）。
6. **闭包里改外部变量**要加锁 —— 闭包捕获的是变量本身，不是快照。
7. **无缓冲 channel 在同一 goroutine 里收发** → 立即死锁，runtime 会报 `all goroutines are asleep - deadlock!`。
8. **`time.After` 在循环里会泄漏定时器**（Go 1.23 前），循环中改用 `time.NewTimer` + `Reset`。

### 调试工具

```bash
go test -race ./...          # 竞态检测（必用）
go build -race               # 带竞态检测编译
GODEBUG=schedtrace=1000 ./app  # 打印调度器状态
go tool pprof http://localhost:6060/debug/pprof/goroutine  # goroutine 泄漏排查
```

---

## 四、工程化对照（Maven/Gradle → go mod）

| Maven / Gradle | Go |
|---|---|
| `pom.xml` / `build.gradle` | `go.mod` |
| `mvn install` | `go mod download` / `go mod tidy` |
| `~/.m2/repository` | `$GOPATH/pkg/mod`（默认 `~/go/pkg/mod`） |
| `mvn spring-boot:run` | `go run .` / `go run ./cmd/api` |
| `mvn package` → jar | `go build -o app` → **单个静态可执行文件** |
| `mvn test` | `go test ./...` |
| `mvn verify` / Checkstyle | `go vet ./...`、`staticcheck`、`golangci-lint` |
| JUnit + Mockito | 标准库 `testing` + 手写 fake；`testify` 可选 |
| `src/main/java`、`src/test/java` | **同目录**下 `xxx.go` 和 `xxx_test.go` |
| `groupId:artifactId:version` | `module github.com/you/project` + git tag |
| 多模块聚合 pom | 多 module + `go.work`（1.18+），或单 module 多 package |
| `application.yml` + `@ConfigurationProperties` | 环境变量 + `flag` + `viper`（**标准库不读配置文件**） |
| `java -jar app.jar` | `./app`（**无运行时依赖**，镜像可做到 ~10MB） |
| Lombok | 不需要（字段直接导出） |
| Spring AOP / Filter | 中间件函数 `func(http.Handler) http.Handler` |
| `-Xmx` / `-Xms` 调优 | `GOGC`（默认 100）、`GOMEMLIMIT`（1.19+）、pprof |
| `mvn dependency:tree` | `go mod graph` / `go mod why` |

### 项目布局（社区事实标准，非官方强制）

```
project/
├── go.mod
├── cmd/
│   └── api/
│       └── main.go          # 入口，≈ @SpringBootApplication
├── internal/                # 【编译器强制】只有本 module 能 import
│   ├── handler/             # ≈ Controller
│   ├── service/             # ≈ Service
│   ├── repository/          # ≈ DAO / Mapper
│   └── model/               # ≈ Entity / DTO
├── pkg/                     # 允许外部 import 的公共库（有争议，非必需）
├── configs/
└── Makefile                 # 可选，统一常用命令
```

`internal/` 是 Go 编译器**真正强制**的可见性边界 —— 比 Java 的包命名约定硬得多，是"模块化"最有效的工具。

### 常用命令

```bash
go mod init github.com/you/gostudy   # ≈ 创建 pom.xml
go get github.com/gin-gonic/gin      # ≈ 添加依赖（自动写入 go.mod）
go mod tidy                          # ≈ 清理无用依赖 + 补全缺失依赖
go build ./...                       # 编译所有包
go build -o bin/api ./cmd/api        # 编译指定入口
go test -race -cover ./...           # 测试 + 竞态 + 覆盖率
go test -run TestFoo -v ./internal/...  # 跑指定测试
go vet ./...                         # 官方静态检查
gofmt -l -w .                        # 格式化（-l 只列出，-w 写回）
go run ./cmd/api                     # 直接运行
cross-env GOOS=linux GOARCH=amd64 go build -o bin/api-linux ./cmd/api  # 交叉编译
```

### 依赖管理要点

- **不需要 `go get` 装到全局**，依赖是 per-module 的，声明在 `go.mod`。
- `go.sum` 是校验和锁文件，**必须提交**到 git（类似 `package-lock.json`）。
- 版本用 git tag：`v1.2.3`。没有 `1.2.3-SNAPSHOT` 这种概念。
- **主版本号 ≥2 时，module 路径要带后缀**：`github.com/foo/bar/v2`。这是 Go 独有的规则。
- 私有仓库：`go env -w GOPRIVATE=github.com/yourcompany/*`。

---

## 五、标准库 vs Java 类库

| 需求 | Java | Go |
|---|---|---|
| HTTP 服务端 | Spring Boot / Tomcat / Jetty | **`net/http` 内置**（生产可用），Gin / Echo / Chi |
| HTTP 客户端 | HttpClient / OkHttp | `net/http`（内置） |
| JSON | Jackson / Gson | `encoding/json`（内置，靠 struct tag） |
| 配置 | Spring `application.yml` | `os.Getenv` + `flag` + `viper`/`koanf` |
| 日志 | SLF4J + Logback | `log/slog`（1.21+ 内置结构化日志） |
| 时间 | `java.time` | `time`（`Time` / `Duration` / `Timer`） |
| 正则 | `java.util.regex` | `regexp`（**RE2 引擎，不支持回溯引用**） |
| 集合工具 | Guava / Stream API | `slices`、`maps` 泛型包（1.21+）+ for 循环 |
| Stream API | `stream().filter().map()` | **没有**，老实写 for |
| UUID | `UUID.randomUUID()` | `github.com/google/uuid`（标准库无） |
| 数据库 | JDBC / MyBatis / JPA | `database/sql` + `sqlx` / `gorm` / `sqlc` |
| 连接池 | HikariCP | `database/sql` 内置池（`SetMaxOpenConns` 等） |
| 校验 | Bean Validation | `go-playground/validator`（靠 struct tag） |
| 测试断言 | JUnit + AssertJ | 标准库 `testing` + `testify`（可选） |
| Mock | Mockito | **手写 fake 实现接口**为主，`gomock` 可选 |
| 缓存 | Caffeine / Redis 客户端 | `sync.Map` 本地；`go-redis` / `ristretto` |
| 消息队列 | Spring AMQP / Kafka Client | `segmentio/kafka-go` / `rabbitmq/amqp091-go` |
| 依赖注入 | Spring IoC / Guice | **没有容器**，手写 `NewXxx(dep1, dep2)` |

**重点提醒：Go 没有 Stream API。** Java 里那套链式集合操作，Go 里就是 for 循环，而且社区认为这样更可读、更容易调试：

```java
// Java
List<String> names = users.stream()
    .filter(u -> u.getAge() > 18)
    .map(User::getName)
    .collect(Collectors.toList());
```

```go
// Go
var names []string
for _, u := range users {
    if u.Age > 18 {
        names = append(names, u.Name)
    }
}
```

Go 1.21+ 的 `slices` / `maps` 包提供了 `slices.Contains`、`slices.SortFunc`、`slices.IndexFunc`、`maps.Keys`、`maps.Values` 等，但**没有惰性求值的流**。

**JSON 靠 struct tag，不靠注解处理器**：

```go
type User struct {
    ID        int64     `json:"id"`
    Name      string    `json:"name"`
    Email     string    `json:"email,omitempty"`   // 零值时省略
    Password  string    `json:"-"`                 // 永不序列化
    CreatedAt time.Time `json:"created_at"`
}
```

---

## 六、Web 服务最小对照

**Java (Spring Boot)**：

```java
@RestController
@RequestMapping("/users")
public class UserController {
    @Autowired private UserService svc;

    @GetMapping("/{id}")
    public User get(@PathVariable Long id) {
        return svc.findById(id)
                  .orElseThrow(() -> new NotFoundException("user not found"));
    }
}
```

**Go（标准库 `net/http`，1.22+ 路由）**：

```go
func main() {
    mux := http.NewServeMux()
    mux.HandleFunc("GET /users/{id}", getUser) // 1.22+ 支持方法和路径变量
    log.Fatal(http.ListenAndServe(":8080", mux))
}

func getUser(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")

    u, err := store.Find(r.Context(), id)
    if err != nil {
        http.Error(w, "user not found", http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(u)
}
```

**对比 Spring 的三点本质差异**：

1. **没有注解、没有反射魔法、没有 IoC 容器。**
   依赖靠构造函数显式注入：

   ```go
   type UserHandler struct {
       svc *UserService // 小写 = 私有
   }

   func NewUserHandler(svc *UserService) *UserHandler {
       return &UserHandler{svc: svc}
   }
   ```

2. **Handler 就是一个接口，一个方法**：

   ```go
   type Handler interface {
       ServeHTTP(w http.ResponseWriter, r *http.Request)
   }

   // http.HandlerFunc 是最妙的技巧：让普通函数满足接口
   type HandlerFunc func(w http.ResponseWriter, r *http.Request)

   func (f HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
       f(w, r)
   }
   ```

   看懂这个 10 行代码，你就理解了 Go 接口设计的精髓 —— 这也是**中间件**的基础：

   ```go
   // 中间件就是：接收 Handler，返回 Handler
   func Logging(next http.Handler) http.Handler {
       return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
           start := time.Now()
           next.ServeHTTP(w, r)               // 前置逻辑
           slog.Info("req", "path", r.URL.Path, "cost", time.Since(start)) // 后置逻辑
       })
   }

   // 链式组合 ≈ Spring 的 Filter Chain，但没有 XML/注解，就是函数嵌套
   handler := Logging(Recover(Auth(mux)))
   ```

3. **显式优于隐式。** 在 Java 里 Spring 帮你 `@Autowired`；在 Go 里你会写很多 `NewXxx(dep1, dep2)`。看起来啰嗦，但读代码时依赖关系一目了然，启动时也没有魔法。

---

## 七、Java 开发者 Top 12 踩坑清单

按"踩坑频率 × 迷惑程度"排序：

1. **忘记检查 `err`** —— Go 编译器不管你，`errcheck` linter 会管。每个返回 error 的调用都要处理或显式 `_ =` 忽略。
2. **接口 != nil 陷阱** —— 详见 [§1.3](#3-接口是隐式实现的鸭子类型)。
3. **值接收者改不了原对象** —— 想改就得用 `*T` 接收者。
4. **`nil` map 写入 panic** —— 必须 `make`。
5. **循环里 `defer`** —— 攒到函数返回才执行，可能耗尽 fd。
6. **goroutine 泄漏** —— 每个 `go` 都要有退出路径。
7. **向已关闭的 channel 发送 / 重复 close** —— panic。
8. **`range` 遍历 map 顺序随机** —— Go 故意打乱，别依赖顺序（需要有序就排序 key）。
9. **`len(string)` 是字节数不是字符数** —— 中文场景必踩。
10. **`append` 可能返回新底层数组** —— `a = append(a, x)` 必须接收返回值；共享底层数组时修改会互相影响。
11. **`sync.WaitGroup` 值拷贝** —— 传参要 `*sync.WaitGroup`。
12. **`time.Time` 用 `==` 比较** —— 时区和单调时钟问题，要用 `.Equal()`。

---

## 参考

- 官方规范：[The Go Programming Language Specification](https://go.dev/ref/spec)
- 官方教程：[A Tour of Go](https://go.dev/tour/)
- 常见错误：[50 Shades of Go](https://golang50shades.com/)
- 风格指南：[Google Go Style Guide](https://google.github.io/styleguide/go/)
- 并发模式：[Go Concurrency Patterns (Rob Pike)](https://go.dev/blog/io2010-pike)
- 上下文：[Go Concurrency Patterns: Context](https://go.dev/blog/context)

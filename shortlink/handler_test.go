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

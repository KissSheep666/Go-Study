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

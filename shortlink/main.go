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

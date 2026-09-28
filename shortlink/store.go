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

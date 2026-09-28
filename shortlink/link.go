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

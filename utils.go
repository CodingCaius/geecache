package geecache

import (
	"fmt"
	"runtime"
	"strings"
)

// 显示错误时运行堆栈
func trace(errorMessage string) string {
	var pcstack [32]uintptr
	n := runtime.Callers(3, pcstack[:])

	//使用Builder优化速度
	var str strings.Builder
	str.WriteString(errorMessage + "\nTraceback:")
	for _, pc := range pcstack[:n] {
		function := runtime.FuncForPC(pc)
		file, line := function.FileLine(pc)
		str.WriteString(fmt.Sprintf("\n\t%s:%d", file, line))
	}
	return str.String()
}

// 判断是否满足 x.x.x.x:port 的格式
func validPeerAddr(addr string) bool {
	token1 := strings.Split(addr, ":")
	if len(token1) != 2 {
		return false
	}
	token2 := strings.Split(token1[0], ".")
	if token1[0] != "localhost" && len(token2) != 4 {
		return false
	}
	return true
}
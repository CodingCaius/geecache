// 缓存值的抽象与封装

package geecache

// A ByteView holds an immutable view of bytes.
type ByteView struct {
	b []byte // b 将会存储真实的缓存值
}

// 实现value接口
// Len returns the view's length
func (v ByteView) Len() int {
	return len(v.b)
}

// b 是只读的，使用 ByteSlice() 方法返回一个拷贝，防止缓存值被外部程序修改
// ByteSlice returns a copy of the data as a byte slice.
func (v ByteView) ByteSlice() []byte {
	return cloneBytes(v.b)
}

// String 将字节数据转换为字符串，并返回该字符串。与 ByteSlice 方法类似，这里也会创建一个字符串的副本，以保持不可变性
func (v ByteView) String() string {
	return string(v.b)
}

func cloneBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
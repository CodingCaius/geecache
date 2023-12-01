// 缓存值的抽象与封装

package geecache

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"time"
)

// A ByteView holds an immutable view of bytes.
// Internally it wraps either a []byte or a string,
// but that detail is invisible to callers.
//
// A ByteView is meant to be used as a value type, not
// a pointer (like a time.Time).

// A ByteView holds an immutable view of bytes.
type ByteView struct {
	// If b is non-nil, b is used, else s is used.
	b []byte // b 将会存储真实的缓存值
	s string
	e time.Time
}

// 返回与该视图关联的过期时间
func (v ByteView) Expire() time.Time {
	return v.e
}

// 实现value接口
// Len returns the view's length
func (v ByteView) Len() int {
	if v.b != nil {
		return len(v.b)
	}
	return len(v.s)
}

// b 是只读的，使用 ByteSlice() 方法返回一个拷贝，防止缓存值被外部程序修改
// ByteSlice returns a copy of the data as a byte slice.
func (v ByteView) ByteSlice() []byte {
	if v.b != nil {
		return cloneBytes(v.b)
	}
	return []byte(v.s)
}

// String 将字节数据转换为字符串，并返回该字符串。与 ByteSlice 方法类似，这里也会创建一个字符串的副本，以保持不可变性
func (v ByteView) String() string {
	if v.b != nil {
		return string(v.b)
	}
	return v.s
}

// At returns the byte at index i.
func (v ByteView) At(i int) byte {
	if v.b != nil {
		return v.b[i]
	}
	return v.s[i]
}

// Slice 在提供的 from 和 to 索引之间对视图进行切片,创建一个新的 ByteView
func (v ByteView) Slice(from, to int) ByteView {
	if v.b != nil {
		return ByteView{b: v.b[from:to]}
	}
	return ByteView{s: v.s[from:to]}
}

// SliceFrom 从提供的索引开始切片视图，创建一个新的 ByteView
func (v ByteView) SliceFrom(from int) ByteView {
	if v.b != nil {
		return ByteView{b: v.b[from:]}
	}
	return ByteView{s: v.s[from:]}
}

// Copy 将 b 复制到 dest 并返回复制的字节数。
func (v ByteView) Copy(dest []byte) int {
	if v.b != nil {
		return copy(dest, v.b)
	}
	return copy(dest, v.s)
}

// 检查当前 ByteView 中的字节是否等于另一个 ByteView。
func (v ByteView) Equal(b2 ByteView) bool {
	if b2.b == nil {
		return v.EqualString(b2.s)
	}
	return v.EqualBytes(b2.b)
}

func (v ByteView) EqualString(s string) bool {
	if v.b == nil {
		return v.s == s
	}
	l := v.Len()
	if len(s) != l {
		return false
	}
	for i, bi := range v.b {
		if bi != s[i] {
			return false
		}
	}
	return true
}

func (v ByteView) EqualBytes(b2 []byte) bool {
	if v.b != nil {
		return bytes.Equal(v.b, b2)
	}
	l := v.Len()
	if len(b2) != l {
		return false
	}
	for i, bi := range b2 {
		if bi != v.s[i] {
			return false
		}
	}
	return true
}

/*
Reader() 方法的目的是提供一个标准的读取接口，使得调用方可以像处理其他实现了 io.Reader 接口的对象一样，方便地读取 ByteView 中的数据。这对于一些操作，比如在缓存中读取数据或者将数据写入到其他 io.Writer 中，提供了一种一致的方式。
*/
// Reader 返回一个实现了 io.ReadSeeker 接口的对象，该对象允许读取 ByteView 中的字节数据，并支持定位读取
func (v ByteView) Reader() io.ReadSeeker {
	if v.b != nil {
		return bytes.NewReader(v.b)
	}
	return strings.NewReader(v.s)
}

// ReadAt 实现了 io.ReaderAt 接口，允许通过指定偏移量在 ByteView 中的字节上进行读取。
func (v ByteView) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, errors.New("view: invalid offset")
	}
	if off >= int64(v.Len()) {
		return 0, io.EOF
	}
	n = v.SliceFrom(int(off)).Copy(p)
	if n < len(p) {
		err = io.EOF
	}
	return
}

/*
WriteTo 方法提供了将 ByteView 中的字节数据写入到目标 io.Writer 中的标准化接口，使得 ByteView 可以与实现了 io.Writer 接口的各种目标协同工作，例如文件、网络连接等。
*/
// 实现了 io.WriterTo 接口，允许将 ByteView 中的字节数据写入到实现了 io.Writer 接口的目标中。
func (v ByteView) WriteTo(w io.Writer) (n int64, err error) {
	var m int
	if v.b != nil {
		m, err = w.Write(v.b)
	} else {
		m, err = io.WriteString(w, v.s)
	}
	if err == nil && m < v.Len() {
		// 写入不完整
		err = io.ErrShortWrite
	}
	n = int64(m)
	return
}
/*
Sink 是一个接口类型，包含了 `SetString`, `SetBytes` `SetProto` `view` 四种方法，每个实现该接口的 Struct 也额外实现了 SetView。这些方法的目的是将值保存成 `byteview` 的格式。

 SetString 方法： 设置存储的值为字符串
 SetBytes 方法： 设置存储的值为字节数组
 SetProto 方法： 设置存储的值为 Protocol Buffers 格式的消息 。Protocol Buffers 是一种二进制序列化格式，通常用于在不同语言之间交换结构化数据。
 view 方法： 用于获取存储的数据的一个冻结（不可修改）的视图，返回一个 ByteView 对象

当在一致性哈希环上的节点中需要保存值时，`Sink` 接口的实现将会被使用。实现 `Sink` 接口的结构体需要提供具体的逻辑，将值以适当的格式存储，并且可能在需要时返回 `byteview` 对象，以方便读取和使用这些值。

在 groupcache 中，`Sink` 接口的实现可以用于与底层存储系统（如缓存、数据库等）进行交互，从而将数据有效地保存和检索。
*/

package geecache

import (
	"time"

	"google.golang.org/protobuf/proto"
)

// 这些声明允许编译器检查这些类型是否真正实现了 Sink 接口的所有方法。
// 如果没有正确实现，编译时会产生错误。
var _ Sink = &stringSink{}
var _ Sink = &allocBytesSink{}
var _ Sink = &protoSink{}
var _ Sink = &truncBytesSink{}
var _ Sink = &byteViewSink{}

// Sink 从 Get 调用接收数据。
//
// 如果 Getter 接口定义了 SetString、SetBytes、SetProto 这样的方法，
// 那么在成功获取到数据后，实现类应该调用其中的一个方法，将获取到的数据设置到缓存中。
// 这样做的好处是，下次相同的请求可以直接从缓存中获取数据，而不需要再次去获取远程数据，
// 减轻了对远程资源的访问压力，提高了性能和效率。
//
// `e` 设置一个可选的未来时间，该时间将在该时间到期。
// 如果您不想过期，请传递零值
// `time.Time`（例如，`time.Time{}`）。
type Sink interface {
	// SetString 设置存储的值为字符串
	SetString(s string, e time.Time) error

	// SetBytes sets the value to the contents of v.
	// The caller retains ownership of v.
	SetBytes(v []byte, e time.Time) error

	// SetProto sets the value to the encoded version of m.
	// The caller retains ownership of m.
	SetProto(m proto.Message, e time.Time) error

	// view 获取存储的数据的一个冻结（不可修改）的视图，返回一个 ByteView 对象
	view() (ByteView, error)
}

func cloneBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// 这个函数的目的是提供一个灵活的数据设置接口，通过尽量减少复制操作，最大限度地利用已缓存数据的内存优势。这在处理本地缓存时可以提高性能。
func setSinkView(s Sink, v ByteView) error {
	// A viewSetter is a Sink that can also receive its value from
	// a ByteView. This is a fast path to minimize copies when the
	// item was already cached locally in memory (where it's
	// cached as a ByteView)
	type viewSetter interface {
		setView(v ByteView) error
	}
	// 如果对象 s 实现了特殊的 viewSetter 接口，那么它就能够直接接收 ByteView，这时就调用 setView 方法，避免了额外的复制。这是一种特殊的快速路径。
	if vs, ok := s.(viewSetter); ok {
		return vs.setView(v)
	}
	// 下面是正常流程
	// 如果 ByteView 中的字节切片不为空（v.b != nil），说明数据在本地内存中已经缓存了，就调用 s.SetBytes 方法，将字节切片和过期时间传递给 SetBytes。
	if v.b != nil {
		return s.SetBytes(v.b, v.Expire())
	}
	return s.SetString(v.s, v.Expire())
}

// StringSink 返回一个实现了 Sink 接口的对象
func StringSink(sp *string) Sink {
	return &stringSink{sp: sp}
}

type stringSink struct {
	// 在 Go 中，字符串是不可变的，意味着一旦创建，就不能更改其内容。
	// 为了支持在 SetString 方法中修改传入的字符串值，使用了字符串指针。
	sp *string // 字符串指针，指向传入的需保存的字符串的值
	v  ByteView
	// TODO(bradfitz): track whether any Sets were called.
}

// view 返回一个 ByteView，表示当前 stringSink 中保存的数据的视图。
func (s *stringSink) view() (ByteView, error) {
	// TODO(bradfitz): return an error if no Set was called
	return s.v, nil
}

// 将传入的字符串值 v 设置到 stringSink 中, 同时更新相关的状态信息
func (s *stringSink) SetString(v string, e time.Time) error {
	s.v.b = nil
	s.v.s = v
	// 将传入的字符串值 v 设置到 stringSink 结构体中的字符串指针 sp 所指向的位置。这表示该字符串指针所指向的字符串会被更新为传入的值。
	*s.sp = v
	s.v.e = e
	// 表示 SetString 方法执行成功，没有发生错误。
	return nil
}

// SetBytes 将传入的字节数组保存到 stringSink 中，保存为字符串形式
func (s *stringSink) SetBytes(v []byte, e time.Time) error {
	return s.SetString(string(v), e)
}

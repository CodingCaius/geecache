/*
Sink 是一个接口类型，包含了 `SetString`, `SetBytes` `SetProto` `view` 四种方法，每个实现该接口的 Struct 也额外实现了 setView。这些方法的目的是将值保存成 `byteview` 的格式。

 SetString 方法： 将传入的数据存储为字符串
 SetBytes 方法： 将传入的数据存储为字节数组
 SetProto 方法： 将传入的数据存储为 Protocol Buffers 格式的消息。Protocol Buffers 是一种二进制序列化格式，通常用于在不同语言之间交换结构化数据。
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


// 上面是接口的定义
// 下面是接口的三种实现，分别用于处理不同类型的消息



// StringSink 返回一个实现了 Sink 接口的对象
func StringSink(sp *string) Sink {
	return &stringSink{sp: sp}
}

// 主要用于处理字符串类型的数据
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

// SetProto 将传入的Protocol Buffers消息存储到字节数组和字符串中
func (s *stringSink) SetProto(m proto.Message, e time.Time) error {
	b, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	s.v.b = b
	// 将字节切片 b 转换为字符串，并将其赋值给 stringSink 接收者 (s) 的 sp 字段指向的值
	*s.sp = string(b)
	s.v.e = e
	return nil
}




// 返回一个 Sink 实例，用于填充 ByteView
func ByteViewSink(dst *ByteView) Sink {
	if dst == nil {
		panic("nil dst")
	}
	return &byteViewSink{dst: dst}
}

// 实现了 Sink 接口，主要用于存储 字节数字 类型数据
type byteViewSink struct {
	dst *ByteView
	/*
	虽然在某些情况下强调 set* 方法只能调用一次，但在实际使用中，如果有多个处理器或者程序中的多个函数需要使用同一个 Sink，并且多次调用 set* 方法并不会引起问题，那么就不必将多次调用看作是错误。
	*/
}

// 提供了一种快速设置 byteViewSink 目标字段的方式，直接使用传入的 ByteView 值，而无需额外的处理或转换。
func (s *byteViewSink) setView(v ByteView) error {
	*s.dst = v
	return nil
}

func (s *byteViewSink) view() (ByteView, error) {
	return *s.dst, nil
}

func (s *byteViewSink) SetProto(m proto.Message, e time.Time) error {
	b, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	*s.dst = ByteView{b: b, e: e}
	return nil
}

func (s *byteViewSink) SetBytes(b []byte, e time.Time) error {
	// 字节数组的赋值操作实际上是复制了引用，而不是字节数组的内容
	// 由于 cloneBytes(b) 返回的是 b 的副本，即一个新的字节切片，
	// 因此在后续的操作中，外部修改原始字节切片 b 不会影响 *s.dst。
	*s.dst = ByteView{b: cloneBytes(b), e: e}
	return nil
}

func (s *byteViewSink) SetString(v string, e time.Time) error {
	*s.dst = ByteView{s: v, e: e}
	return nil
}



func ProtoSink(m proto.Message) Sink {
	return &protoSink{
		dst: m,
	}
}

// 主要处理 Protocol Buffers 消息
type protoSink struct {
	// proto.Message 是 Protocol Buffers 库中定义的一个接口，所有 Protocol Buffers 消息类型都实现了这个接口。
	// 保存了消息实际的值
	dst proto.Message
	typ string

	v ByteView // encoded
}

func (s *protoSink) view() (ByteView, error) {
	return s.v, nil
}

func (s *protoSink) SetBytes(b []byte, e time.Time) error {
	// 将 Protocol Buffers 消息的二进制表示形式（字节切片）反序列化为相应的消息类型。
	err := proto.Unmarshal(b, s.dst)
	if err != nil {
		return err
	}
	s.v.b = cloneBytes(b)
	s.v.s = ""
	s.v.e = e
	return nil
}

func (s *protoSink) SetString(v string, e time.Time) error {
	b := []byte(v)
	err := proto.Unmarshal(b, s.dst)
	if err != nil {
		return err
	}
	s.v.b = b
	s.v.s = ""
	s.v.e = e
	return nil
}

// SetProto 将 Protocol Buffers 消息转换为二进制形式(字节数组形式)
func (s *protoSink) SetProto(m proto.Message, e time.Time) error {
	// 将 Protocol Buffers 消息 m 转换为二进制形式（字节切片 b）
	b, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	// TODO(bradfitz): optimize for same-task case more and write
	// right through? would need to document ownership rules at
	// the same time. but then we could just assign *dst = *m
	// here. This works for now:

	// 将二进制形式反序列化到 protoSink 结构体的目标值 dst 中，这样 dst 就包含了 Protocol Buffers 消息的实际值。
	err = proto.Unmarshal(b, s.dst)
	if err != nil {
		return err
	}
	// 将二进制编码保存到 v 字段的 b 字节切片中
	s.v.b = b
	s.v.s = ""
	s.v.e = e
	return nil

	/*
	直接将消息存储到 protoSink 结构体中可能会引发一些问题。proto.Message 是一个接口类型，可能包含多个不同类型的消息。结构体中的 dst 字段是一个 proto.Message 类型的值。如果直接赋值，那么它可能会指向一个完全不同类型的消息。

	通过 proto.Marshal(m) 将消息序列化为二进制形式，确保了消息的统一表示形式。
	这样，即使消息的类型发生变化，其二进制表示形式仍然是一致的。

	通过 proto.Unmarshal(b, s.dst) 将二进制形式反序列化回 protoSink 结构体中的 dst 字段。
	这个步骤保证了 dst 字段确切地包含了消息的实际类型和值。

	*/
}


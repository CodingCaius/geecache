// 负责与外部交互，控制缓存存储和获取的主流程

// 该模块提供比cache模块更高一层抽象的能力
// 换句话说，实现了填充缓存/命名划分缓存的能力

// 包groupcache提供了带缓存的数据加载机制
// 以及跨一组对等进程运行的重复数据删除。
//
// 每个数据Get首先查阅其本地缓存，否则委托
// 发送给所请求密钥的规范所有者，然后检查其缓存
// 或者最终获取数据。 在常见情况下，许多并发
// 同一键的一组对等点之间的缓存未命中仅导致
// 一次缓存填充。

// 主要作用是在一组对等进程之间提供数据加载和缓存，以降低对底层数据源的访问压力

/*
这个包的设计允许多个对等进程共享缓存，并且通过Getter接口加载数据，从而可以轻松地适应不同的数据源。
整个系统的关键点是Group结构体，它管理着本地和远程缓存，并通过Getter加载数据。
*/

package geecache

import (
	"context"
	"fmt"
	"log"
	"sync"

	pb "github.com/CodingCaius/geecache/geecachepb"
	"github.com/CodingCaius/geecache/singleflight"
	"github.com/sirupsen/logrus"
)

// 一个全局的日志变量 logger，类型为 Logger。这是在整个包内使用的日志记录器。
var logger Logger

// SetLogger - 这是为了提供与 logrus 的向后兼容性而遗留的。
func SetLogger(log *logrus.Entry) {
	logger = LogrusLogger{Entry: log}
}

// SetLoggerFromLogger  将 logger 设置为 实现了 Logger 接口的实例
// 允许使用不同的日志库，只要它实现了 Logger 接口，而不仅仅局限于 logrus
func SetLoggerFromLogger(log Logger) {
	logger = log
}

// Getter  加载键的数据
type Getter interface {
	// Get 返回由 key 标识的值，填充 dest。
	// 返回的数据必须是无版本的。 也就是说，关键必须
	// 唯一地描述加载的数据，没有隐式的当前时间，并且不依赖缓存过期机制。
	Get(ctx context.Context, key string, dest Sink) error
}

// GetterFunc 用函数实现 Getter。
type GetterFunc func(ctx context.Context, key string, dest Sink) error

// 函数类型 GetterFunc 实现 Getter 接口
func (f GetterFunc) Get(ctx context.Context, key string, dest Sink) error {
	return f(ctx, key, dest)
}

// GetterFunc 通过实现 Get 方法，使得任意匿名函数func
// 通过被GetterFunc(func)类型强制转换后，实现了 Getter 接口的能力
// 通过这种方式，我们可以以函数的形式定义数据加载逻辑，并将其用作 Getter 接口的实现。这提供了更灵活的方式来定制数据加载过程，

var (
	mu     sync.RWMutex              // 读写互斥锁，用于保护对全局变量 groups 的并发访问
	groups = make(map[string]*Group) // 用于存储缓存组（Group）对象。这个映射的键是缓存组的名称（字符串），值是对应的 Group 对象。
	// 这样的设计允许在整个程序中通过名称检索已创建的缓存组。

	initPeerServerOnce sync.Once // 用于确保 initPeerServer 函数只被执行一次。
	// sync.Once 是一种在并发环境中执行初始化逻辑的常用方式，保证在多次调用中只有第一次会执行，以避免重复初始化。

	initPeerServer func()
	// 函数变量，用于存储在第一次创建缓存组时执行的初始化逻辑。
)

// 用来特定名称的 Group，这里使用了只读锁 RLock()，因为不涉及任何冲突变量的写操作
// GetGroup 返回先前使用 NewGroup 创建的命名组，
// 如果没有这样的组则为 nil
// GetGroup 获取对应的 命名空间
func GetGroup(name string) *Group {
	mu.RLock()
	g := groups[name]
	mu.RUnlock()
	return g
}

// 用于创建一个协调的、具备组意识的 Getter 对象。
// NewGroup 用于创建一个 Group 对象，该对象实现了缓存组的协同工作。
// newGroup 函数接受四个参数，其中第四个参数是 PeerPicker 接口的实例，用于选择对等节点。在 NewGroup 中，此参数被设为 nil，表示没有指定对等节点选择器。
func NewGroup(name string, cacheBytes int64, getter Getter) *Group {
	return newGroup(name, cacheBytes, getter, nil)
}

// 从全局的缓存组池中移除指定名称的缓存组
func DeregisterGroup(name string) {
	mu.Lock() //获取全局的读写互斥锁
	delete(groups, name)
	mu.Unlock()
}

// 如果peers为nil，则通过sync.Once调用peerPicker来初始化它。
func newGroup()






// 一个 Group 可以认为是一个缓存的命名空间，每个 Group 拥有一个唯一的名称 name。
// 比如可以创建三个 Group，缓存学生的成绩命名为 scores，缓存学生信息的命名为 info，缓存学生课程的命名为 courses
// A Group is a cache namespace and associated data loaded spread over
type Group struct {
	name      string
	getter    Getter // 缓存未命中时获取源数据的回调(callback)
	mainCache cache  // 并发缓存
	peers     PeerPicker
	// use singleflight.Group to make sure that
	// each key is only fetched once
	loader *singleflight.Group // 处理重复请求
}

// NewGroup 实例化 Group，创建一个新的缓存空间，并且将 group 存储在全局变量 groups 中
func NewGroup(name string, cacheBytes int64, getter Getter) *Group {
	// 为了确保创建的缓存组具有有效的数据获取方式，不允许传入一个空的 getter
	if getter == nil {
		panic("nil Getter")
	}
	mu.Lock()
	defer mu.Unlock()
	g := &Group{
		name:      name,
		getter:    getter,
		mainCache: cache{cacheBytes: cacheBytes},
		loader:    &singleflight.Group{},
	}
	groups[name] = g
	return g
}

// Get value for a key from cache
func (g *Group) Get(key string) (ByteView, error) {
	if key == "" {
		return ByteView{}, fmt.Errorf("key is required")
	}

	// 从 mainCache 中查找缓存，如果存在则返回缓存值
	if v, ok := g.mainCache.get(key); ok {
		log.Println("[GeeCache] hit")
		return v, nil
	}

	// 缓存不存在，则调用 load 方法，
	// load 调用 getLocally（分布式场景下会调用 getFromPeer 从其他节点获取），getLocally 调用用户回调函数 g.getter.Get() 获取源数据，并且将源数据添加到缓存 mainCache 中（通过 populateCache 方法）
	return g.load(key)
}

// load 从缓存中加载数据，首先尝试从远程节点获取，如果失败则从本地缓存获取
func (g *Group) load(key string) (value ByteView, err error) {
	// 每个密钥仅获取一次（本地或远程）
	//无论并发呼叫者的数量如何。
	viewi, err := g.loader.Do(key, func() (interface{}, error) {
		if g.peers != nil {
			if peer, ok := g.peers.PickPeer(key); ok {
				if value, err = g.getFromPeer(peer, key); err == nil {
					return value, nil
				}
				log.Println("[GeeCache] Failed to get from peer", err)
			}
		}

		return g.getLocally(key)
	})

	if err == nil {
		return viewi.(ByteView), nil
	}
	return
}

// 缓存未命中时，调用回调函数获取数据，并填充缓存
func (g *Group) getLocally(key string) (ByteView, error) {
	bytes, err := g.getter.Get(key)
	// 如果获取数据时发生错误，会返回一个空的 ByteView 和相应的错误
	if err != nil {
		return ByteView{}, err

	}
	// 如果数据成功获取，它将获取到的字节数组 bytes 使用 cloneBytes 函数进行克隆，然后创建一个 ByteView 结构体，并将克隆后的字节数组赋值给 ByteView 的 b 字段
	// 这一步之所以要复制字节数组而不是直接传递 bytes，是为了确保数据的不可变性和安全性
	// ，切片（包括字节数组切片）是引用类型，这意味着如果直接传递 bytes，那么后续对 bytes 的任何修改都会影响到原始数据。这可能会导致在缓存中共享的数据被不经意地修改，从而引发不一致的结果或数据损坏
	value := ByteView{b: cloneBytes(bytes)}
	g.populateCache(key, value)
	return value, nil
}

// populateCache 将 key 和对应的 ByteView 存储到缓存中,提供填充缓存的能力
func (g *Group) populateCache(key string, value ByteView) {
	g.mainCache.add(key, value)
}

// RegisterPeers 为 Group 注册远程节点选择器(Server)
func (g *Group) RegisterPeers(peers PeerPicker) {
	// 如果已经注册过节点选择器，会触发 panic
	if g.peers != nil {
		panic("RegisterPeerPicker called more than once")
	}
	g.peers = peers
}

// getFromPeer 通过远程节点的 PeerGetter 接口从远程节点获取数据
func (g *Group) getFromPeer(peer ProtoGetter, key string) (ByteView, error) {
	req := &pb.GetRequest{
		Group: g.name,
		Key:   key,
	}
	res := &pb.GetResponse{}
	err := peer.Get(req, res)
	if err != nil {
		return ByteView{}, err
	}
	return ByteView{b: res.Value}, nil
}

// DestoryGroup 销毁指定组名的缓存组，停止相关服务器
func DestoryGroup(name string) {
	g := GetGroup(name)
	if g != nil {
		svr := g.peers.(*server)
		svr.Stop()
		delete(groups, name)
		log.Printf("Destroy cache [%s %s]", name, svr.addr)
	}
}

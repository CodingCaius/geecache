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
	"errors"
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
	// 该函数在 PeerServer 启动时被调用
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
func newGroup(name string, cacheBytes int64, getter Getter, peers PeerPicker) *Group {
	// 为了确保创建的缓存组具有有效的数据获取方式，不允许传入一个空的 getter
	if getter == nil {
		panic("nil Getter")
	}
	mu.Lock()
	defer mu.Unlock()
	// 用 sync.Once 来确保初始化对等体服务器的操作（callInitPeerServer）只执行一次。
	initPeerServerOnce.Do(callInitPeerServer)
	// 检查是否已经注册了具有相同名称的组，如果是，则触发 panic，表示不允许重复注册相同名称的组。
	if _, dup := groups[name]; dup {
		panic("duplicate registration of group " + name)
	}
	g := &Group{
		name:        name,
		getter:      getter,
		peers:       peers,
		cacheBytes:  cacheBytes,
		loadGroup:   &singleflight.Group{},
		setGroup:    &singleflight.Group{},
		removeGroup: &singleflight.Group{},
	}
	// 如果存在注册的新组钩子函数（newGroupHook），则调用该函数，并将新创建的组作为参数传递给它。这允许在创建组时执行额外的自定义逻辑。
	if fn := newGroupHook; fn != nil {
		fn(g)
	}
	groups[name] = g
	return g
}

// newGroupHook，钩子函数，如果非零，则在创建新组后立即调用。
// 每次创建新组时运行
var newGroupHook func(*Group)

// RegisterNewGroupHook 注册一个每次创建组时运行的钩子。
func RegisterNewGroupHook(fn func(*Group)) {
	if newGroupHook != nil {
		panic("RegisterNewGroupHook called more than once")
	}
	newGroupHook = fn
}

// RegisterServerStart 注册一个在创建第一个组时运行的钩子。
func RegisterServerStart(fn func()) {
	if initPeerServer != nil {
		panic("RegisterServerStart called more than once")
	}
	initPeerServer = fn
}

// 调用 initPeerServer 函数，该函数在 PeerServer 启动时被调用。
func callInitPeerServer() {
	// 避免在空指针的情况下调用该函数而导致程序崩溃
	if initPeerServer != nil {
		initPeerServer()
	}
}

// 一个 Group 可以认为是一个缓存的命名空间，每个 Group 拥有一个唯一的名称 name。
// 比如可以创建三个 Group，缓存学生的成绩命名为 scores，缓存学生信息的命名为 info，缓存学生课程的命名为 courses
// A Group 是一个缓存命名空间，加载的相关数据分布在一组或多台机器上。
type Group struct {
	// 缓存组的名称，用于标识不同的缓存命名空间
	name string

	// 实现了 Getter 接口的加载器，用于根据键加载数据。
	getter Getter // 缓存未命中时获取源数据的回调(callback)

	// 确保对等体初始化的同步对象，保证初始化操作只执行一次
	peersOnce sync.Once

	// 实现了 PeerPicker 接口的对等体选择器，用于选择负责特定键的对等体。
	peers PeerPicker

	// 限制 mainCache 和 hotCache 大小总和
	cacheBytes int64

	// 包含对于当前进程及其对等体而言是有权威的键的缓存。这个缓存包含一致性哈希到当前进程的对等体号码的键。
	// 对于当前进程来说，权威数据是通过 Getter 接口加载的，然后存储在 mainCache 中
	mainCache cache // 并发缓存

	// hotCache 包含该对等点不具有权威性的键/值（否则它们将位于 mainCache 中），但足够流行以保证在此过程中进行镜像，以避免通过网络从对等点获取。 拥有 hotCache 可以避免网络热点，在这种情况下，对等方的网卡可能会成为流行键的瓶颈。
	// 谨慎使用此缓存，以最大化可全局存储的键/值对的总数。
	hotCache cache

	// loadGroup 确保每个键仅获取一次（本地或远程），无论并发调用者数量如何。
	loader flightGroup // 处理重复请求

	// setGroup 确保每个添加的 key 只远程添加一次，无论并发调用者数量有多少。
	setGroup flightGroup

	//removeGroup 确保每个被删除的键只被远程删除一次，无论并发调用者的数量有多少。
	removeGroup flightGroup

	// 一个匿名的、占位的 int32 类型字段，没有实际的用途，只是为了填充字节，使得 Stats 结构体在 32 位平台上的整体大小达到 8 的倍数。
	_ int32

	// 包含了对该缓存组的统计信息，如获取次数、缓存命中次数、对等体加载次数等。
	Stats Stats
}

// FlightGroup 被定义为 flightgroup.Group 满足的接口。
// 即 singleflight的结构体满足的接口
type flightGroup interface {
	Do(key string, f func() (any, error)) (any, error)
	Lock(fn func())
}

// 用于记录 groupcache 缓存组的统计信息的结构体
type Stats struct {
	// 记录任何 Get 请求的总数，包括来自对等节点的请求
	Gets AtomicInt

	// 记录缓存命中的总数，表示在本地缓存中找到了请求的数据
	CacheHits AtomicInt

	// 记录从对等节点请求数据的最慢持续时间。这表示在从对等节点获取数据时所花费的最长时间。
	GetFromPeersLatencyLower AtomicInt

	// 记录远程加载或远程缓存命中的总数，表示从对等节点获取数据的次数，不包括错误的情况。
	PeersLoads AtomicInt

	// 记录从对等节点获取数据时发生的错误的总数。
	PeerErrors AtomicInt

	// 记录总的加载次数，计算方式为 Gets - CacheHits，表示所有加载数据的次数，包括本地加载和远程加载。
	Loads AtomicInt

	// 记录经过 singleflight 机制去重后的加载次数，表示实际执行加载操作的次数，避免了相同数据的并发加载。
	LoadsDeduped AtomicInt

	// 记录本地成功加载数据的总次数，表示从本地缓存获取数据的次数。
	LocalLoads AtomicInt

	// 记录本地加载数据时发生错误的总次数，表示从本地缓存获取数据失败的次数。
	LocalLoadErrs AtomicInt

	// 记录的是该节点向其他节点发起的网络请求的总数
	ServerRequests AtomicInt
}

// AtomicInt 是一个可以原子访问的 int64。
type AtomicInt int64

// Name returns the name of the group.
func (g *Group) Name() string {
	return g.name
}

// 初始化缓存组的节点选择器，初始化用于选择对等节点的机制
func (g *Group) initPeers() {
	if g.peers == nil {
		g.peers = getPeers(g.name)
	}
}

// Get 方法用于从缓存中获取数据
// 如果缓存命中，直接返回缓存中的数据
// 如果缓存未命中，根据情况从对等节点或本地加载数据，并将加载到的数据设置到目标 Sink 中。
// 在整个过程中，对缓存命中和未命中的情况进行了统计。
func (g *Group) Get(ctx context.Context, key string, dest Sink) error {
	g.peersOnce.Do(g.initPeers)
	g.Stats.Gets.Add(1)
	if dest == nil {
		return errors.New("groupcache: nil dest Sink")
	}
	// 从缓存中查找数据
	value, cacheHit := g.lookupCache(key)

	if cacheHit {
		g.Stats.CacheHits.Add(1)
		// 将缓存中的数据设置到目标 Sink 中
		return setSinkView(dest, value)
	}

	// 处理缓存未命中的情况

	// 初始化一个标志，表示目标 Sink 是否已经被填充
	destPopulated := false
	// 从对等节点或本地加载数据，填充目标 Sink
	value, destPopulated, err := g.load(ctx, key, dest)
	if err != nil {
		return err
	}
	// 如果目标 Sink 已经被填充 (destPopulated)，则返回 nil，表示操作成功。
	if destPopulated {
		return nil
	}
	// 否则，调用 setSinkView(dest, value) 将加载到的数据设置到目标 Sink 中，并返回 nil，表示操作成功。
	return setSinkView(dest, value)
}

// Set 设置键值对到缓存中
func (g *Group) Set(ctx context.Context, key string, value []byte, expire time.Time, hotCache bool) error {
	// 初始化用于选择对等节点的机制
	g.peersOnce.Do(g.initPeers)

	if key == "" {
		return errors.New("empty Set() key not allowed")
	}

	// 使用 g.setGroup.Do 方法确保对于相同的 key，只有一个请求在执行
	_, err := g.setGroup.Do(key, func() (interface{}, error) {
		// 如果远程对等体拥有该 key
		owner, ok := g.peers.PickPeer(key)
		if ok {
			// 通过远程对等体设置 key 的值
			if err := g.setFromPeer(ctx, owner, key, value, expire); err != nil {
                return nil, err
            }
			// 如果需要，将值更新到本地缓存中
            if hotCache {
                g.localSet(key, value, expire, &g.hotCache)
            }
            return nil, nil
		}

		// 如果当前节点拥有该 key，则将值设置到本地缓存中
        g.localSet(key, value, expire, &g.mainCache)
        return nil, nil
	})
	// 返回可能出现的错误
    return err
}

// Remove 会从缓存中清除密钥，然后将删除请求转发给所有对等点。
func (g *Group) Remove(ctx context.Context, key string) error {
	g.peersOnce.Do(g.initPeers)

	_, err := g.removeGroup.Do(key, func() (interface{}, error) {
		// 首先从 key 所属的对等体移除
        owner, ok := g.peers.PickPeer(key)
		if ok {
			if err := g.removeFromPeer(ctx, owner, key); err != nil {
				return nil, err
			}
		}
		// 然后从本地缓存中移除
        g.localRemove(key)

		// 异步地清除 key 在所有对等体的主缓存和热缓存中的值
        wg := sync.WaitGroup{}
        errs := make(chan error)

		for _, peer := range g.peers.GetAll() {
            // 避免重复从 key 所属的对等体删除
            if peer == owner {
                continue
            }

            wg.Add(1)
            go func(peer ProtoGetter) {
                errs <- g.removeFromPeer(ctx, peer, key)
                wg.Done()
            }(peer)
        }

		go func() {
            wg.Wait()
            close(errs)
        }()

		// TODO(thrawn01): 是否应该报告所有错误？每个对等体的上下文取消错误报告不太有意义。
        var err error
        for e := range errs {
            err = e
        }

        return nil, err
	})
	return err
}


// load 通过本地调用 getter 或将其发送到另一台机器来加载密钥。
func (g *Group) load(ctx context.Context, key string, dest Sink) (value ByteView, destPopulated bool, err error) {
	// 增加统计信息，表示有一个加载操作。
	g.Stats.Loads.Add(1)

	// 保证每个键只被获取一次
	viewi, err := g.loadGroup.Do(key, func() (interface{}, error) {
		// 再次检查缓存，因为 singleflight 只能删除同时重叠的调用。 2 个并发请求可能会错过缓存，从而导致 2 个 load() 调用。 不幸的 goroutine 调度会导致该回调连续运行两次。 如果我们不再检查缓存，即使该键只有一个条目，cache.nbytes 也会增加到以下。
		// 考虑两个 goroutine 的以下序列化事件排序，其中针对相同的键调用此回调两次：
		// 1: 获取(“密钥”)
		// 2: 获取(“密钥”)
		// 1:lookupCache("key")
		// 2:lookupCache("key")
		// 1：加载（“键”）
		// 2：加载（“键”）
		// 1: loadGroup.Do("key", fn)
		// 1: fn()
		// 2: loadGroup.Do("key", fn)
		// 2: fn()
	})

	if err != nil {
		value = viewi.(ByteView)
	}
	return
}








// // Get value for a key from cache
// func (g *Group) Get(key string) (ByteView, error) {
// 	if key == "" {
// 		return ByteView{}, fmt.Errorf("key is required")
// 	}

// 	// 从 mainCache 中查找缓存，如果存在则返回缓存值
// 	if v, ok := g.mainCache.get(key); ok {
// 		log.Println("[GeeCache] hit")
// 		return v, nil
// 	}

// 	// 缓存不存在，则调用 load 方法，
// 	// load 调用 getLocally（分布式场景下会调用 getFromPeer 从其他节点获取），getLocally 调用用户回调函数 g.getter.Get() 获取源数据，并且将源数据添加到缓存 mainCache 中（通过 populateCache 方法）
// 	return g.load(key)
// }

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

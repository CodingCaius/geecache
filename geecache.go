// 负责与外部交互，控制缓存存储和获取的主流程

// 该模块提供比cache模块更高一层抽象的能力
// 换句话说，实现了填充缓存/命名划分缓存的能力

package geecache

import (
	pb "github.com/CodingCaius/geecache/geecachepb"
	"github.com/CodingCaius/geecache/singleflight"
	"fmt"
	"log"
	"sync"
)

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

// Getter  加载键的数据
type Getter interface {
	Get(key string) ([]byte, error)
}

// A GetterFunc implements Getter with a function.
type GetterFunc func(key string) ([]byte, error)

// GetterFunc 通过实现 Get 方法，使得任意匿名函数func
// 通过被GetterFunc(func)类型强制转换后，实现了 Getter 接口的能力

// Get implements Getter interface function
func (f GetterFunc) Get(key string) ([]byte, error) {
	return f(key)
}

var (
	mu     sync.RWMutex
	groups = make(map[string]*Group)
)

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

// 用来特定名称的 Group，这里使用了只读锁 RLock()，因为不涉及任何冲突变量的写操作
// GetGroup returns the named group previously created with NewGroup, or
// nil if there's no such group.
// GetGroup 获取对应命名空间的缓存
func GetGroup(name string) *Group {
	mu.RLock()
	g := groups[name]
	mu.RUnlock()
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
func (g *Group) getFromPeer(peer PeerGetter, key string) (ByteView, error) {
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

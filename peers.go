
// 缓存系统中节点的定义
// 主要涉及两个接口 ProtoGetter 和 PeerPicker
// 以及 注册初始化函数，根据组名创建缓存组相关联的 peer（PeerPicker 实例）


package geecache

import (
	"context"

	pb "github.com/CodingCaius/geecache/geecachepb"
)

// 在这里，抽象出 2 个接口，

// PeerPicker 的 PickPeer() 方法用于根据传入的 key 选择相应节点 ProtoGetter
// 接口 ProtoGetter 的 Get() 方法用于从对应 group 查找缓存值。
// ProtoGetter 就对应于上述流程中的 HTTP 客户端。

// ProtoGetter 接口定义了 peer 的基本操作，而 PeerPicker 接口定义了如何选择和定位 peers 以有效地处理缓存请求。

// ProtoGetter 是每个 peer 必须实现的接口。
// ProtoGetter  定义了 peer 必须实现的一组方法，这些方法用于获取、删除和设置缓存项，以及获取 peer 的 URL
type ProtoGetter interface {
	// 从 peer 获取缓存项的值
	Get(context context.Context, in *pb.GetRequest, out *pb.GetResponse) error
	// 从 peer 删除缓存项
	Remove(context context.Context, in *pb.GetRequest) error
	// 将缓存项设置到 peer 上
	Set(context context.Context, in pb.SetRequest) error
	// GetURL 获取 peer 的 URL
	GetURL() string
}
// 通过实现 ProtoGetter 接口，每个 peer 可以成为缓存系统的一部分，负责处理缓存项的获取、删除和设置操作，并提供自己的标识（URL）

// 实现 ProtoGetter 接口时，可以选择使用不同的方法签名，
// 只要确保实现了 ProtoGetter 接口的 Get 方法的名字和 proto 文件中定义的一样即可。
// 因为 gRPC 生成的代码在内部会处理输入和输出参数的映射。

// PeerPicker是定位必须实现的接口
// 拥有特定密钥的对等点。
// PeerPicker 定义了用于定位拥有特定键的 peer 的方法，以及获取所有 peers 的方法
type PeerPicker interface {
	// 根据键选择拥有该键的 peer，并返回该 peer 以及一个标志指示是否是远程 peer。如果键的所有权属于当前 peer，则返回 nil 和 false
	PickPeer(key string) (peer ProtoGetter, ok bool)
	// 返回缓存组中的所有 peers
	GetAll() []ProtoGetter
}


// 在某些情况下，系统无法找到任何可用的 peer
// NoPeers is an implementation of PeerPicker that never finds a peer.
type NoPeers struct{}
func (NoPeers) PickPeer(key string) (peer ProtoGetter, ok bool) { return }
func (NoPeers) GetAll() []ProtoGetter {return []ProtoGetter{}}

/*
这种实现主要用于处理一些特殊情况，例如在系统初始化阶段或者在某些配置下，缓存系统可能不涉及到实际的分布式 peer，或者因为某些原因无法找到可用的 peer。这样的实现允许系统在没有可用 peer 时保持正常运行，而不会引发错误。
*/


// 函数变量
// 保存 peer 初始化函数
// 可以根据需要创建与缓存组相关联的 PeerPicker 实例
var (
	portPicker func(groupName string) PeerPicker
)

// 注册  peer 初始化函数
// 在创建第一个缓存组时调用，确保只调用一次
// 要么调用 RegisterPeerPicker，要么调用 RegisterPerGroupPeerPicker，但不能两者都调用。
//  如果 portPicker 已经被赋值（不为 nil），则抛出 panic，防止重复调用。否则，将 portPicker 赋值为 传入的 peer 初始化函数，并返回该函数。
func RegisterPeerPicker(fn func() PeerPicker) {
	if portPicker != nil {
		panic("RegisterPeerPicker called more than once")
	}
	portPicker = func(_ string) PeerPicker { return fn() }
}

// 注册 带有组名参数的 peer 初始化函数
// 在创建第一个缓存组时调用，确保只调用一次
// 要么调用 RegisterPeerPicker，要么调用 RegisterPerGroupPeerPicker，但不能两者都调用
// 如果 portPicker 已经被赋值（不为 nil），则抛出 panic，防止重复调用。否则，将 portPicker 赋值为传入的带有组名参数的 peer 初始化函数，并返回该函数
func RegisterPerGroupPeerPicker(fn func(groupName string) PeerPicker) {
	if portPicker != nil {
		panic("RegisterPeerPicker called more than once")
	}
	portPicker = fn
}

// 根据给定的组名创建相应的 PeerPicker 实例
// 根据给定的组名，通过调用已注册的 peer 初始化函数（如果存在），获取与缓存组相关联的 PeerPicker 实例。
// 如果无法获取有效的 PeerPicker，则返回一个 NoPeers{} 实例，表示没有可用的 peer。
// 这种设计允许系统在运行时根据需要获取相应的 peer 实例，以便进行缓存操作。
func getPeers(groupName string) PeerPicker {
	// 检查全局变量 portPicker 是否为 nil，即是否已经注册了 peer 初始化函数。如果没有注册，说明无法获取可用的 peer 初始化函数，因此返回一个 NoPeers{} 的实例，表示没有可用的 peer。
	if portPicker == nil {
		return NoPeers{}
	}
	// 如果 portPicker 不为 nil，则调用 portPicker 函数，传递组名 groupName，以获取与缓存组相关联的 PeerPicker 实例
	// 组名传递给注册的 peer 初始化函数，然后由这个函数负责创建与指定组相关联的 PeerPicker 实例
	pk := portPicker(groupName)
	// 如果是空的，将 pk 重新赋值为 NoPeers{}，表示没有可用的 peer
	if pk == nil {
		pk = NoPeers{}
	}
	// 如果成功获取到有效的 PeerPicker，则返回该实例
	return pk
}
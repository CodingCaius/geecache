package geecache

import pb "github.com/CodingCaius/geecache/geecachepb"

// 在这里，抽象出 2 个接口，
// PeerPicker 的 PickPeer() 方法用于根据传入的 key 选择相应节点 PeerGetter。
// 接口 PeerGetter 的 Get() 方法用于从对应 group 查找缓存值。
// PeerGetter 就对应于上述流程中的 HTTP 客户端。

// PeerPicker是定位必须实现的接口
// 拥有特定密钥的对等点。
type PeerPicker interface {
	PickPeer(key string) (peer PeerGetter, ok bool)
}

// PeerGetter 是每个 peer 必须实现的接口。
type PeerGetter interface {
	Get(in *pb.Request, out *pb.Response) error
}

// 实现 PeerGetter 接口时，可以选择使用不同的方法签名，
// 只要确保实现了 PeerGetter 接口的 Get 方法的名字和 proto 文件中定义的一样即可。
// 因为 gRPC 生成的代码在内部会处理输入和输出参数的映射。

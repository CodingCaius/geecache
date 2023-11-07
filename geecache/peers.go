package geecache

import pb "cache/geecache/geecachepb"

// 在这里，抽象出 2 个接口，
// PeerPicker 的 PickPeer() 方法用于根据传入的 key 选择相应节点 PeerGetter。
// 接口 PeerGetter 的 Get() 方法用于从对应 group 查找缓存值。
// PeerGetter 就对应于上述流程中的 HTTP 客户端。

// PeerPicker是定位必须实现的接口
// 拥有特定密钥的对等点。
type PeerPicker interface {
	PickPeer(key string) (peer PeerGetter, ok bool)
}

// PeerGetter 是对等方必须实现的接口。
type PeerGetter interface {
	Get(in *pb.Request, out *pb.Response) error
}

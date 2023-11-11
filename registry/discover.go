package registry

import (
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/resolver"
	"google.golang.org/grpc"
)

// EtcdDial 向grpc请求一个服务
// 用于在 gRPC 客户端中建立连接的函数，通过提供一个etcd client和service name即可获得Connection
func EtcdDail(c *clientv3.Client, service string) (*grpc.ClientConn, error) {
	// 创建一个 etcd 解析器
	// 该解析器用于解析服务名称到实际地址的映射
	etcdResolver, err := resolver.NewBuilder(c)
	if err != nil {
		return nil, err
	}

	// 1 , creds := insecure.NewCredentials()
	return grpc.Dial(
		// 使用 "etcd:///" 前缀来告诉 gRPC 使用 etcd 解析器
		"etcd:///"+service,
		grpc.WithResolvers(etcdResolver),
		// 创建一个不安全的 gRPC 连接，即在通信中不使用传输层的安全性
		// 不推荐
		grpc.WithInsecure(), //  2 , grpc.WithTransportCredentials(creds),
		// 阻塞直到连接成功建立
		grpc.WithBlock(),
	)
}
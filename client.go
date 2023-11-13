// client 模块实现 访问其他远程节点，从而获取缓存数据 的功能
package geecache

import (
	"context"
	"fmt"
	pb "github.com/CodingCaius/geecache/geecachepb"
	"github.com/CodingCaius/geecache/registry"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)


// 用于访问其他远程节点的客户端
type client struct {
	name string // 服务名称 geecache/ip:port
}

// Get 从remote peer获取对应缓存值,借助 etcd 进行服务发现，通过 gRPC 进行通信，处理错误并返回结果
func (c *client) Get(in *pb.Request, out *pb.Response) error {
	// 创建 etcd 客户端
	cli, err := clientv3.New(defaultEtcdConfig)
	if err != nil {
		return err
	}
	defer cli.Close()

	// 发现服务，取得与服务的连接
	conn, err := registry.EtcdDial(cli, c.name)
	if err != nil {
		return err
	}
	defer conn.Close()

	// 创建 gRPC 客户端
	grpcClient := pb.NewGeeCacheClient(conn)
	// 构建 gRPC 请求上下文
	// 创建了一个带有超时的上下文，确保 gRPC 请求在规定的时间内完成。
	// defer cancel() 用于在函数返回前取消上下文，释放相关资源
	ctx, cancel := context.WithTimeout(context.Background(), 10 * time.Second)
	defer cancel()

	// 发起 gRPC 请求
	resp, err := grpcClient.Get(ctx, &pb.Request{
		Group: in.Group,
		Key: in.Key,
	})
	if err != nil {
		return fmt.Errorf("could not get %s/%s from peer %s", in.Group, in.Key, c.name)
	}

	out.Value = resp.GetValue()
	return nil
}

func NewClient(service string) *client {
	return &client{name: service}
}

// 测试 Client 是否实现了 PeerGetter 接口
var _ PeerGetter = (*client)(nil)

// 服务注册模块，
// 提供了服务注册的功能，将服务的信息注册到etcd

package registry

import (
	"context"
	"fmt"
	"log"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/endpoints"
)

// 预定义了一个默认的etcd配置，包括etcd服务器的地址和连接超时
var (
	defaultEtcdConfig = clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	}
)

// 用于在租赁模式下向etcd添加服务端点
// 它创建了一个endpoints.Manager，然后使用该管理器添加服务端点，同时关联了租约ID
// etcdAdd 在租赁模式添加一对kv至etcd
func etcdAdd(c *clientv3.Client, lid clientv3.LeaseID, service string, addr string) error {
	// 创建了一个用于管理服务的端点
	em, err := endpoints.NewManager(c, service)
	if err != nil {
		return err
	}
	// 将服务端点添加到 etcd 中
	// 	c.Ctx(): 上下文，用于控制请求的生命周期。
	// service+"/"+addr: 构建服务端点的键，通常是服务名称加上地址。
	// endpoints.Endpoint{Addr: addr}: 表示要添加的端点的信息，包括地址。
	// clientv3.WithLease(lid): 使用指定的租约 ID，将端点关联到租约。这是租约模式的一部分，确保在租约过期后自动清理服务端点。
	return em.AddEndpoint(c.Ctx(), service+"/"+addr, endpoints.Endpoint{Addr: addr}, clientv3.WithLease(lid))
}

// Register 注册一个服务至etcd
// 并且通过租约机制实现服务的心跳检测和自动清理
// 注意 Register将不会return 如果没有error的话
func Register(service string, addr string, stop chan error) error {
	// 创建一个 etcd 客户端
	cli, err := clientv3.New(defaultEtcdConfig)
	if err != nil {
		return fmt.Errorf("creat etcd client failed: %v", err)
	}
	defer cli.Close()

	// 创建一个租约 配置5秒过期
	resp, err := cli.Grant(context.Background(), 5)
	if err != nil {
		return fmt.Errorf("creat lease failed: %v", err)
	}
	leaseId := resp.ID

	// 注册服务
	err = etcdAdd(cli, leaseId, service, addr)
	if err != nil {
		return fmt.Errorf("add etcd record failed: %v", err)
	}

	// 设置服务心跳检测
	// 使用 cli.KeepAlive 设置了服务的心跳检测。这会创建一个用于保持租约活跃的通道 (ch)
	ch, err := cli.KeepAlive(context.Background(), leaseId)
	if err != nil {
		return fmt.Errorf("set keepalive failed: %v", err)
	}
	log.Printf("[%s] register service ok\n", addr)

	// 	最后，函数进入一个无限循环，等待各种事件：
	// 如果 stop 通道被关闭，表示停止服务，函数返回。
	// 如果 etcd 客户端的上下文 (cli.Ctx()) 被关闭，表示服务关闭，函数返回。
	// 如果从 ch 通道接收到消息，表示租约的心跳继续，函数继续等待。
	for {
		select {
		case err := <-stop:
			if err != nil {
				log.Println(err)
			}
			return err
		case <-cli.Ctx().Done():
			log.Println("service closed")
			return nil
		case _, ok := <-ch:
			// 监听租约
			if !ok {
				log.Println("keep alive channel closed")
				_, err := cli.Revoke(context.Background(), leaseId)
				return err
			}
		}
	}

}

// 实现了一个基本的 gRPC 缓存服务器，具有启动、停止、处理请求、一致性哈希等功能


package geecache

import (
	"context"
	"fmt"
	"github.com/CodingCaius/geecache/consistenthash"
	pb "github.com/CodingCaius/geecache/geecachepb"
	"github.com/CodingCaius/geecache/registry"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
)

// server 模块为 geecache 之间提供通信能力
// 这样部署在其他机器上的cache可以通过访问server获取缓存
// 至于找哪台主机 那是一致性哈希的工作了

const (
	defaultAddr = "127.0.0.1:8080"
	defaultReplicas = 50
)

var (
	defaultEtcdConfig = clientv3.Config{
		Endpoints: []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	}
)

// server 和 Group 是解耦合的 所以server要自己实现并发控制
// server 结构体定义了 geecache 的 gRPC 服务
type server struct {
	// 这玩意儿必须写
	pb.UnimplementedGeeCacheServer // 未实现的 gRPC 服务接口，用于扩展 gRPC 方法

	addr string // 服务器的地址，格式为 ip:port
	status bool // 服务器的运行状态，true 表示正在运行，false 表示停止
	stopSignal chan error // 用于通知 register 停止 keep alive 服务
	mu sync.Mutex // 互斥锁，用于保护 server 结构体的并发访问
	consHash *consistenthash.Map // 一致性哈希，用于选择节点
	clients map[string]*client // 用于存储 缓存节点的客户端,键是缓存节点的地址（格式为 ip:port），值是对应节点的客户端对象
}

// NewServer 创建cache的svr 若addr为空 则使用defaultAddr
func NewServer(addr string) (*server, error) {
	if addr == "" {
		addr = defaultAddr
	}
	if !validPeerAddr(addr) {
		return nil, fmt.Errorf("invalid addr %s, it should be x.x.x.x:port", addr)
	}
	return &server{addr: addr}, nil
}

// Get 实现了 geecachepb.proto 文件中 GroupCache 接口的 Get 方法，用于处理 gRPC 请求
func (s *server) Get(ctx context.Context, in *pb.GetRequest) (*pb.GetResponse, error) {
	group, key := in.GetGroup(), in.GetKey()
	resp := &pb.GetResponse{}

	log.Printf("[geecache_svr %s] Recv RPC Request - (%s)/(%s)", s.addr, group, key )
	if key == "" {
		return resp, fmt.Errorf("key required")
	}
	g := GetGroup(group)
	if g == nil {
		return resp, fmt.Errorf("group not found")
	}
	view, err := g.Get(key)
	if err != nil {
		return resp, err
	}
	resp.Value = view.ByteSlice()
	return resp, nil
}

// Start 启动缓存服务，包括监听指定地址的 TCP 连接和注册服务至 etcd
func (s *server) Start() error {
	// 获取服务器状态的互斥锁，以确保在对状态进行更改时不会被其他 goroutine 干扰
	s.mu.Lock()
	if s.status == true {
		s.mu.Unlock()
		return fmt.Errorf("server already started")
	}
	// -----------------启动服务----------------------
	// 1. 设置status为true 表示服务器已在运行
	// 2. 初始化stop channal,这用于通知registry stop keep alive
	// 3. 初始化tcp socket并开始监听
	// 4. 注册rpc服务至grpc 这样grpc收到request可以分发给server处理
	// 5. 将自己的服务名/Host地址注册至etcd 这样client可以通过etcd
	//    获取服务Host地址 从而进行通信。这样的好处是client只需知道服务名
	//    以及etcd的Host即可获取对应服务IP 无需写死至client代码中
	// ----------------------------------------------

	s.status = true
	s.stopSignal = make(chan error)

	port := strings.Split(s.addr, ":")[1]
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	// 创建一个新的 gRPC 服务器并将缓存服务注册到该服务器上
	grpcServer := grpc.NewServer()
	pb.RegisterGeeCacheServer(grpcServer, s)

	// 注册服务至 etcd
	go func() {
		// 将服务注册到 etcd。这个操作是阻塞的，直到接收到 s.stopSignal 信号，或者注册发生错误。这是一个阻塞调用
		err := registry.Register("geecache", s.addr, s.stopSignal)
		if  err != nil {
			log.Fatalf(err.Error())
		}
		// Close channel
		// 当服务注册完成或出现错误时，关闭 s.stopSignal 通道。这个操作将通知其他等待停止信号的 goroutine，服务注册已完成或出现了错误
		// // 注意 Register将不会return 如果没有error的话
		close(s.stopSignal)
		// Close tcp listen
		err = lis.Close()
		if err != nil {
			log.Fatalf(err.Error())
		}
		log.Printf("[%s] Revoke service and close tcp socket ok.", s.addr)
	}()

	s.mu.Unlock()

	// 启动 gRPC 服务器开始监听 gRPC 请求。它是一个阻塞操作，会一直运行直到服务停止或发生错误。
	// 当有新的 gRPC 请求到达时，它将调用之前注册的 gRPC 处理函数来处理请求。
	if err := grpcServer.Serve(lis); s.status && err != nil {
		return fmt.Errorf("failed to serve: %v", err)
	}
	return nil
}

// SetPeers 将各个远端主机IP配置到Server里
// 这样Server就可以Pick他们了
// 注意: 此操作是*覆写*操作！
// 注意: peersIP必须满足 x.x.x.x:port的格式
func (s *server) SetPeers(peersAddr ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 这个一致性哈希对象用于根据键选择缓存节点
	s.consHash = consistenthash.New(defaultReplicas, nil)
	// 将提供的远端主机地址注册到一致性哈希中
	s.consHash.Add(peersAddr...)
	// 创建客户端对象
	s.clients = make(map[string]*client)
	for _, peerAddr := range peersAddr {
		if !validPeerAddr(peerAddr) {
			panic(fmt.Sprintf("[peer %s] invalid address format, it should be x.x.x.x:port", peerAddr))
		}
		service := fmt.Sprintf("geecache/%s", peerAddr)
		s.clients[peerAddr] = NewClient(service)
	}
}

// Pick 根据键选择合适的节点来获取缓存数据
// return false 代表从本地获取cache
func (s *server) PickPeer(key string) (PeerGetter, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	peerAddr := s.consHash.Get(key)
	// 如果选的节点是自身，无需通过网络通信来获取缓存
	if peerAddr == s.addr {
		log.Printf("ooh! pick myself, I am %s\n", s.addr)
		return nil, false
	}
	log.Printf("[cache %s] pick remote peer: %s\n", s.addr, peerAddr)
	return s.clients[peerAddr], true
}

// Stop 停止server运行 如果server没有运行 这将是一个no-op
func (s *server) Stop() {
	s.mu.Lock()
	if s.status == false {
		s.mu.Unlock()
		return
	}
	s.stopSignal <- nil // 发送停止 keep alive 信号
	s.status = false // 设置 server 运行状态为 stop
	s.clients = nil
	s.consHash = nil // 清空信息，有助于垃圾回收
	s.mu.Unlock()
}

// 测试 Server 是否实现了 PeerPicker 接口
var _ PeerPicker = (*server)(nil)

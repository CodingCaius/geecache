// 提供被其他节点访问的能力(基于http)

// 在 GeeCache 第三天 中我们为 HTTPPool 实现了服务端功能，通信不仅需要服务端还需要客户端，因此，我们接下来要为 HTTPPool 实现客户端的功能

// 至此，HTTPPool 既具备了提供 HTTP 服务的能力，也具备了根据具体的 key，创建 HTTP 客户端从远程节点获取缓存值的能力。

package geecache

import (
	"cache/geecache/consistenthash"
	pb "cache/geecache/geecachepb"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
)

const (
	defaultBasePath = "/_geecache/"
	defaultReplicas = 50
)

// HTTPPool 为 HTTP 对等点池实现了 PeerPicker。
type HTTPPool struct {
	// this peer's base URL, e.g. "https://example.net:8000"
	self     string // 用来记录自己的地址，包括主机名/IP 和端口
	basePath string // 作为节点间通讯地址的前缀，默认是 /_geecache/，那么 http://example.com/_geecache/ 开头的请求，就用于节点间的访问
	// 因为一个主机上还可能承载其他的服务，加一段 Path 是一个好习惯。比如，大部分网站的 API 接口，一般以 /api 作为前缀

	// 为 HTTPPool 添加节点选择的功能

	mu          sync.Mutex             // guards peers and httpGetters
	peers       *consistenthash.Map    // 类型是一致性哈希算法的 Map，用来根据具体的 key 选择节点
	httpGetters map[string]*httpGetter // 映射远程节点与对应的 httpGetter。每一个远程节点对应一个 httpGetter，因为 httpGetter 与远程节点的地址 baseURL 有关
}

// NewHTTPPool 初始化 HTTP 对等点池
func NewHTTPPool(self string) *HTTPPool {
	return &HTTPPool{
		self:     self,
		basePath: defaultBasePath,
	}
}

// 带有服务器名称的日志信息
func (p *HTTPPool) Log(format string, v ...interface{}) {
	log.Printf("[Server %s] %s", p.self, fmt.Sprintf(format, v...))
}

// ServeHTTP 处理所有 http 请求
func (p *HTTPPool) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 首先判断访问路径的前缀是否是 basePath，不是返回错误
	if !strings.HasPrefix(r.URL.Path, p.basePath) {
		panic("HTTPPool serving unexpected path: " + r.URL.Path)
	}
	p.Log("%s %s", r.Method, r.URL.Path)
	// /<basepath>/<groupname>/<key> required
	// 这部分代码将请求的路径分割成两部分，使用 / 分割
	// 从 r.URL.Path 中去掉 p.basePath 部分，然后通过 / 分割得到两个部分。
	// 如果分割后的部分不等于2，表示请求路径不符合预期，会返回 HTTP 400 Bad Request 响应
	parts := strings.SplitN(r.URL.Path[len(p.basePath):], "/", 2)
	if len(parts) != 2 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	groupName := parts[0]
	key := parts[1]

	// 获取缓存分组:
	group := GetGroup(groupName)
	if group == nil {
		http.Error(w, "no such group: "+groupName, http.StatusNotFound)
		return
	}

	// 获取缓存数据并返回
	view, err := group.Get(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 使用 Protocol Buffers 库的 proto.Marshal 函数，将缓存数据封装成 Protocol Buffers 格式的消息（pb.Response），然后将该消息的字节表示写入 HTTP 响应体中
	// 样做的目的是将缓存数据以 Protocol Buffers 格式返回给客户端。

	// Write the value to the response body as a proto message.
	body, err := proto.Marshal(&pb.Response{Value: view.ByteSlice()})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 设置响应头并写入响应数据
	w.Header().Set("Content-Type", "application/octet-stream") // 二进制流
	w.Write(body)
}

// Set 用于更新缓存池中的节点列表
func (p *HTTPPool) Set(peers ...string) {
	// 先获取互斥锁，以保证在更新节点列表时不会有其他并发操作
	p.mu.Lock()
	defer p.mu.Unlock()
	// 创建了一个一致性哈希对象 p.peers，并添加了传入的节点列表
	p.peers = consistenthash.New(defaultReplicas, nil)
	p.peers.Add(peers...)
	// 初始化了一个用于存储 HTTPGetter 的映射 p.httpGetters，并为每个节点创建一个对应的 httpGetter 实例
	p.httpGetters = make(map[string]*httpGetter, len(peers))
	for _, peer := range peers {
		p.httpGetters[peer] = &httpGetter{baseURL: peer + p.basePath}
	}
}

// PickPeer 根据给定的键值选择一个节点
func (p *HTTPPool) PickPeer(key string) (PeerGetter, bool) {
	// 先获取互斥锁，以确保在选择节点时不会有其他并发操作
	p.mu.Lock()
	defer p.mu.Unlock()
	// 如果找到的节点不为空且不是当前节点 p.self，则返回对应节点的 httpGetter 实例
	if peer := p.peers.Get(key); peer != "" && peer != p.self {
		p.Log("Pick peer %s", peer)
		return p.httpGetters[peer], true
	}
	return nil, false
}

// 这里实际上是在声明一个匿名变量，目的是为了通过编译时的静态检查
// 如果 HTTPPool 没有实现 PeerPicker 接口的所有方法，编译器会在这一行代码报告错误。这有助于在早期阶段发现潜在的错误，而不是在运行时。
var _ PeerPicker = (*HTTPPool)(nil)

// 创建具体的 HTTP 客户端类 httpGetter，实现 PeerGetter 接口
type httpGetter struct {
	baseURL string // 表示将要访问的远程节点的地址，例如 http://example.com/_geecache/
}

func (h *httpGetter) Get(in *pb.Request, out *pb.Response) error {
	// 将 baseURL 与传入的 group 和 key 参数组合起来，构建了一个 URL 字符串。使用 fmt.Sprintf 进行字符串格式化
	u := fmt.Sprintf(
		"%v%v/%v",
		h.baseURL,
		url.QueryEscape(in.GetGroup()),
		url.QueryEscape(in.GetKey()),
	)

	// 发起一个 HTTP GET 请求到构建好的 URL
	res, err := http.Get(u)
	if err != nil {
		return err
	}
	// defer 语句确保在函数返回后关闭响应体
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned: %v", res.Status)
	}

	// 将响应体读取到一个字节切片中。如果在这个过程中出现错误，就返回一个错误
	bytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %v", err)
	}

	// 使用 Protocol Buffers 库的 proto.Unmarshal 函数，
	// 将字节切片中的二进制数据解码为 Protocol Buffers 格式，并将结果存储在 out 中。
	if err = proto.Unmarshal(bytes, out); err != nil {
		return fmt.Errorf("decoding response body: %v", err)
	}

	return nil
}

var _ PeerGetter = (*httpGetter)(nil)

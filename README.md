GeeCache 仿 groupcache 实现的⼀个分布式缓存，使用gRPC进行通信，用etcd作为服务注册与发现， 并添加了Get 和 Set 接口，及 TTL 机制。

已添加的功能：
- 使用gRPC进行节点间通信，并使用etcd作为服务注册与发现
- 提供了数据更新(Set)和删除(Remove)的⽀持
- 加⼊缓存过期机制
- 基于Logrus实现的日志库可以充分利用Logrus提供的丰富功能，包括结构化日志、多级别支持等


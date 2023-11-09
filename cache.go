// 并发控制

package geecache

import (
	"github.com/CodingCaius/geecache/lru"
	"sync"
)

type cache struct {
	mu         sync.Mutex // 用于确保并发访问时的线程安全
	lru        *lru.Cache // 指向 lru.Cache 类型的指针，用于存储缓存的数据
	cacheBytes int64      // 表示缓存的容量
}

// 向缓存中添加数据。接受两个参数，一个字符串，表示数据的键。一个 ByteView 类型的参数，表示要缓存的数据
func (c *cache) add(key string, value ByteView) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru == nil {
		c.lru = lru.New(c.cacheBytes, nil)
	}
	c.lru.Add(key, value)
}

// 从缓存中获取数据
func (c *cache) get(key string) (value ByteView, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru == nil {
		return
	}

	if v, ok := c.lru.Get(key); ok {
		return v.(ByteView), ok
	}

	return
}

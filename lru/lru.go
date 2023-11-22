// lru 缓存淘汰策略

package lru

import (
	"container/list"
	"time"
)

type NowFunc func() time.Time

// Cache is a LRU cache. It is not safe for concurrent access.
type Cache struct {
	MaxEntries int                      // MaxEntries 是最大缓存条目数,零意味着没有限制。
	ll         *list.List               // 双向链表
	cache      map[any]*list.Element // 字典，键是字符串，值是双向链表中对应节点的指针
	// optional and executed when an entry is purged.
	OnEvicted func(key Key, value any) // 某条记录被移除时的回调函数，可以为 nil

	// Now 是缓存将用来确定的 Now() 函数
	// 用于计算过期值的当前时间
	// 默认为 time.Now()
	Now NowFunc
}

// A Key may be any value that is comparable. See http://golang.org/ref/spec#Comparison_operators
type Key any

// 双向链表节点的数据类型
type entry struct {
	key    Key
	value  any
	expire time.Time
}

// New is the Constructor of Cache
// New creates a new Cache.
// If maxEntries is zero, the cache has no limit and it's assumed
// that eviction is done by the caller.
func New(maxEntries int) *Cache {
	return &Cache{
		MaxEntries: maxEntries,
		ll:         list.New(),
		cache:      make(map[any]*list.Element),
		Now:        time.Now,
	}
}

// Add 向缓存中添加一个数据
func (c *Cache) Add(key any, value any, expire time.Time) {
	// 初始化缓存结构体中的map和双向链表
	if c.cache == nil {
		c.cache = make(map[any]*list.Element)
		c.ll = list.New()
	}
	// 如果键已存在于缓存中，则更新其值和过期时间，并将该条目移到链表头部（表示最近访问）
	if ee, ok := c.cache[key]; ok {
		eee := ee.Value.(*entry)
		if c.OnEvicted != nil {
			c.OnEvicted(key, eee.value)
		}
		c.ll.MoveToFront(ee)
		eee.expire = expire
		eee.value = value
	}

	// 如果键不存在于缓存中，创建一个新的缓存条目并将其添加到链表头部和map中
	ele := c.ll.PushFront(&entry{key, value, expire})
	c.cache[key] = ele

	// 如果设置了最大条目数且当前条目数超过最大值，则移除最老的条目
	if c.MaxEntries != 0 && c.ll.Len() > c.MaxEntries {
		c.RemoveOldest()
	}
}

// Get 从缓存中查找键的值
func (c *Cache) Get(key Key) (value any, ok bool) {
	if c.cache == nil {
		return
	}
	if ele, hit := c.cache[key]; hit {
		entry := ele.Value.(*entry)
		// 如果该条目已过期，则将其从缓存中删除
		if !entry.expire.IsZero() && entry.expire.Before(c.Now()) {
			c.removeElement(ele)
			return nil, false
		}

		c.ll.MoveToFront(ele)
		return entry.value, true
	}
	return
}

// Remove removes the provided key from the cache.
func (c *Cache) Remove(key Key) {
	if c.cache == nil {
		return
	}
	if ele, hit := c.cache[key]; hit {
		c.removeElement(ele)
	}
}

// RemoveOldest 从缓存中删除最旧的项目。
func (c *Cache) RemoveOldest() {
	if c.cache == nil {
		return
	}
	ele := c.ll.Back()
	if ele != nil {
		c.removeElement(ele)
	}
}

func (c *Cache) removeElement(e *list.Element) {
	// 从双向链表中移除指定的链表元素 e
	c.ll.Remove(e)
	// 从缓存的 map 中删除与该链表元素对应的键值对
	kv := e.Value.(*entry)
	delete(c.cache, kv.key)
	// 回调函数
	if c.OnEvicted != nil {
		c.OnEvicted(kv.key, kv.value)
	}
}


// Len returns the number of items in the cache
func (c *Cache) Len() int {
	if c.cache == nil {
		return 0
	}
	return c.ll.Len()
}

// Clear purges all stored items from the cache.
func (c *Cache) Clear() {
	if c.OnEvicted != nil {
		for _, e := range c.cache {
			kv := e.Value.(*entry)
			c.OnEvicted(kv.key, kv.value)
		}
	}
	c.ll = nil
	c.cache = nil
}
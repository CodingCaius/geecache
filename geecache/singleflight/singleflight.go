package singleflight

import "sync"

// call 代表正在进行中，或已经结束的请求。使用 sync.WaitGroup 锁避免重入
// call is an in-flight or completed Do call
type call struct {
	wg  sync.WaitGroup
	val interface{}
	err error
}

// Group represents a class of work and forms a namespace in which
// units of work can be executed with duplicate suppression.
// Group 是 singleflight 的主数据结构，管理不同 key 的请求(call)
type Group struct {
	mu sync.Mutex       // protects m
	m  map[string]*call // 用于存储不同 key 对应的请求信息
}



// 基于互斥锁的并发控制实现，目的是确保在多个 goroutine 并发访问时，对于相同的 key 只有一个 goroutine 能够执行关键部分

// Do 执行并返回给定函数的结果，确保同一时间对于给定的 key 只有一个执行。如果有重复的 key，重复的调用者会等待原始调用者完成，并接收相同的结果
func (g *Group) Do(key string, fn func() (interface{}, error)) (interface{}, error) {
	g.mu.Lock()
	if g.m == nil {
		// 延迟初始化的目的很简单，提高内存使用效率
		g.m = make(map[string]*call)
	}
	// 检查 key 是否已存在于映射中。如果存在，说明已经有其他 goroutine 在处理相同的 key，则释放锁，等待该 goroutine 完成，并返回其结果
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	// 如果 key 不存在于映射中，创建一个新的 call 结构体实例
	c := new(call)
	// 为新创建的 call 实例的等待组增加一个计数，表示有一个 goroutine 正在处理
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	// 调用传入的函数 fn，获取其结果，并将结果保存到 call 实例中
	c.val, c.err = fn()
	// 减少等待组的计数，表示 goroutine 已经完成处理
	c.wg.Done()

	// 再次获取锁，删除映射中的 key，以避免在处理完成后保留无用的 key
	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()

	return c.val, c.err
}

// sync.WaitGroup 是 Go 语言标准库中的一个同步原语，用于等待一组 goroutine 完成其任务。它通常用于等待一组并发操作完成后再继续执行其他操作。

// WaitGroup 主要有三个方法：

// Add(delta int)：增加等待组的计数。delta 是一个正整数，表示要等待的 goroutine 的数量。

// Done()：减少等待组的计数，表示一个 goroutine 已经完成了其任务。

// Wait()：阻塞直到等待组的计数变为零。即等待所有的 Done 调用达到 Add 调用的数量。

// 定义了两个自定义的错误类型 ErrNotFound 和 ErrRemoteCall，这两个错误类型都实现了 Go 语言的 error 接口。
// ErrNotFound 用于指示请求的值在当前节点上不可用，而 ErrRemoteCall 用于指示在远程获取值时发生了错误


package geecache


// ErrNotFound 应该从 `GetterFunc` 的实现中返回，以指示请求的值不可用。
// 当进行远程 HTTP 调用以从其他 groupcache 实例检索值时，返回此错误将向 groupcache 指示请求的值不可用，并且不应尝试在本地调用“GetterFunc”。
type ErrNotFound struct {
	Msg string
}

func (e *ErrNotFound) Error() string {
	return e.Msg
}

func (e *ErrNotFound) Is(target error) bool {
	_, ok := target.(*ErrNotFound)
	return ok
}


// 当远程的 GetterFunc 返回错误时，group.Get() 会返回 ErrRemoteCall，表示在远程获取值时发生了错误。
// 在这种情况下，group.Get()不会尝试通过本地的GetterFunc检索该值。
type ErrRemoteCall struct {
	Msg string
}

func (e *ErrRemoteCall) Error() string {
	return e.Msg
}

func (e *ErrRemoteCall) Is(target error) bool {
	_, ok := target.(*ErrRemoteCall)
	return ok
}
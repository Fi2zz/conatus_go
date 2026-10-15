package session

// Persistence 是会话日志的持久化端口。
//
// 端口只有四个动作（列举 / 载入 / 追加 / 删除），追加是 O(1) 的行写入，载入时逐行
// 反序列化。更换为数据库或对象存储后端时，消费方代码不变。
//
// 与 Dart 版的差别：这里是**同步**接口（Dart 返回 Future）。同步落盘让
// 「Append 返回即已持久化」成立，省掉写入链与 Flush。
type Persistence interface {
	// List 返回已持久化的会话 id（升序）。
	List() ([]string, error)
	// Load 载入某会话的全部事件；不存在时返回空列表。
	Load(id string) ([]Event, error)
	// Append 追加一条事件（append-only）。
	Append(id string, event Event) error
	// Remove 删除某会话的全部持久化数据；不存在不算错误。
	Remove(id string) error
}

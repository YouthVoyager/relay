package engine

import (
	"sync"

	"github.com/YouthVoyager/relay/internal/store/sqlcgen"
)

// Bus 是进程内发布订阅:引擎发布新事件,SSE 连接订阅。
// 投递是尽力而为(best-effort):订阅者跟不上就丢——真相在数据库,重连可补。
type Bus struct {
	mu   sync.RWMutex
	subs map[string]map[chan sqlcgen.Event]struct{} // runID -> 订阅者集合
}

func NewBus() *Bus {
	return &Bus{subs: make(map[string]map[chan sqlcgen.Event]struct{})}
}

// Subscribe 返回接收 ch 和退订函数。buffer 建议 64。
func (b *Bus) Subscribe(runID string, buffer int) (<-chan sqlcgen.Event, func()) {
	ch := make(chan sqlcgen.Event, buffer)
	b.mu.Lock()
	if b.subs[runID] == nil {
		b.subs[runID] = make(map[chan sqlcgen.Event]struct{})
	}
	b.subs[runID][ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		delete(b.subs[runID], ch)
		if len(b.subs[runID]) == 0 {
			delete(b.subs, runID)
		}
		b.mu.Unlock()
	}
	return ch, unsubscribe
}

// Publish 向 runID 的所有订阅者投递。channel 满则丢弃,绝不阻塞。
func (b *Bus) Publish(ev sqlcgen.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs[ev.RunID] {
		select {
		case ch <- ev:
		default: // 满了就丢——数据库兜底,这里不能卡引擎
		}
	}
}
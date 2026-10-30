package support

import (
	"context"
	"sync"
	"time"
)

const (
	// MaxWait 长轮询最长等待；短于 market-api WriteTimeout。
	MaxWait = 20 * time.Second
	// PollTick 跨进程无通知时的 DB 探测间隔。
	PollTick = 300 * time.Millisecond
)

// SyncHint 客户端上次已知状态；任一字段变化即立刻返回。
type SyncHint struct {
	AfterID    int64
	PeerTyping bool
	PeerReadID int64
	Unread     int
	Status     string
}

// 进程内广播：同进程写入后立刻唤醒等待者（C 端 api / 管理端 ServeAdmin 各自一份）。
type eventHub struct {
	mu   sync.Mutex
	all  map[chan struct{}]struct{}
	byID map[int64]map[chan struct{}]struct{}
}

func newHub() *eventHub {
	return &eventHub{
		all:  make(map[chan struct{}]struct{}),
		byID: make(map[int64]map[chan struct{}]struct{}),
	}
}

var hub = newHub()

func (h *eventHub) subscribe(id int64) chan struct{} {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.all[ch] = struct{}{}
	if id > 0 {
		m := h.byID[id]
		if m == nil {
			m = make(map[chan struct{}]struct{})
			h.byID[id] = m
		}
		m[ch] = struct{}{}
	}
	return ch
}

func (h *eventHub) unsubscribe(id int64, ch chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.all, ch)
	if id > 0 {
		if m := h.byID[id]; m != nil {
			delete(m, ch)
			if len(m) == 0 {
				delete(h.byID, id)
			}
		}
	}
}

func (h *eventHub) notify(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	pulse := func(ch chan struct{}) {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	for ch := range h.all {
		pulse(ch)
	}
	if id > 0 {
		for ch := range h.byID[id] {
			pulse(ch)
		}
	}
}

// Notify 会话有变更（消息/已读/输入中），唤醒等待者。
func Notify(conversationID int64) { hub.notify(conversationID) }

func clampWait(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	if d > MaxWait {
		return MaxWait
	}
	return d
}

func (s *Service) waitTick(ctx context.Context, conversationID int64, remaining time.Duration) error {
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	ch := hub.subscribe(conversationID)
	defer hub.unsubscribe(conversationID, ch)
	tick := PollTick
	if remaining < tick {
		tick = remaining
	}
	timer := time.NewTimer(tick)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	case <-timer.C:
		return nil
	}
}

func peerReadOf(c Conversation, viewer string) int64 {
	if viewer == SenderUser {
		return c.StaffLastReadMsgID
	}
	return c.UserLastReadMsgID
}

func unreadOf(c Conversation, viewer string) int {
	if viewer == SenderUser {
		return c.UserUnreadCount
	}
	return c.StaffUnreadCount
}

func bundleChanged(out *ConversationBundle, hint SyncHint, viewer string) bool {
	if out == nil || out.Conversation.ID == 0 {
		return len(out.Messages) > 0
	}
	if len(out.Messages) > 0 {
		return true
	}
	c := out.Conversation
	if c.PeerTyping != hint.PeerTyping {
		return true
	}
	if peerReadOf(c, viewer) != hint.PeerReadID {
		return true
	}
	if unreadOf(c, viewer) != hint.Unread {
		return true
	}
	if hint.Status != "" && c.Status != hint.Status {
		return true
	}
	return false
}

// WaitSyncMine 长轮询用户侧同步。
func (s *Service) WaitSyncMine(ctx context.Context, userID int64, hint SyncHint, wait time.Duration) (*ConversationBundle, error) {
	wait = clampWait(wait)
	deadline := time.Now().Add(wait)
	for {
		out, err := s.SyncMine(ctx, userID, hint.AfterID)
		if err != nil {
			return nil, err
		}
		if wait == 0 || bundleChanged(out, hint, SenderUser) {
			return out, nil
		}
		rem := time.Until(deadline)
		if rem <= 0 {
			return out, nil
		}
		id := out.Conversation.ID
		if err := s.waitTick(ctx, id, rem); err != nil {
			return out, nil
		}
	}
}

// WaitAdminGet 长轮询管理端单会话。
func (s *Service) WaitAdminGet(ctx context.Context, id int64, hint SyncHint, wait time.Duration) (*ConversationBundle, error) {
	wait = clampWait(wait)
	deadline := time.Now().Add(wait)
	for {
		out, err := s.AdminGet(ctx, id, hint.AfterID)
		if err != nil {
			return nil, err
		}
		if wait == 0 || bundleChanged(out, hint, SenderStaff) {
			return out, nil
		}
		rem := time.Until(deadline)
		if rem <= 0 {
			return out, nil
		}
		if err := s.waitTick(ctx, id, rem); err != nil {
			return out, nil
		}
	}
}

// WaitAlerts 长轮询预警数字（顶栏角标）。
func (s *Service) WaitAlerts(ctx context.Context, pending, overdue, unread int, wait time.Duration) (*Alerts, error) {
	wait = clampWait(wait)
	deadline := time.Now().Add(wait)
	for {
		out, err := s.Alerts(ctx)
		if err != nil {
			return nil, err
		}
		if wait == 0 ||
			out.PendingCount != pending ||
			out.OverdueCount != overdue ||
			out.StaffUnreadTotal != unread {
			return out, nil
		}
		rem := time.Until(deadline)
		if rem <= 0 {
			return out, nil
		}
		if err := s.waitTick(ctx, 0, rem); err != nil {
			return out, nil
		}
	}
}

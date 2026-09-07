package main

import (
	"sync"
	"time"
)

// entitlementState 是 Host 所属 Account 的订阅档授权状态。订阅档提供身份、Relay
// 路由与推送；Relay 门禁只看 active/grace 是否生效。
type entitlementState string

const (
	entitlementActive  entitlementState = "active"
	entitlementGrace   entitlementState = "grace"
	entitlementExpired entitlementState = "expired"
	entitlementRevoked entitlementState = "revoked"
	entitlementUnknown entitlementState = "unknown"
)

// allowsRelay 报告该状态是否放行 Relay。active 与 grace 视为"生效"——grace 是
// 欠费前的宽限期，仍保留订阅权益；expired/revoked/unknown 均拒绝，绝不误报 active。
func (s entitlementState) allowsRelay() bool {
	return s == entitlementActive || s == entitlementGrace
}

// entitlementStatus 是推送给前端的订阅状态快照。
type entitlementStatus struct {
	State        entitlementState `json:"state"`
	AccountID    string           `json:"accountID,omitempty"`
	Message      string           `json:"message"`
	ExpiresAt    time.Time        `json:"expiresAt,omitempty"`
	RelayAllowed bool             `json:"relayAllowed"`
}

// entitlementUpdate 是 entitlement stream 推送的一次状态变化。State 只取
// active/grace/expired/revoked；unknown 是本地派生态，不来自 stream。
type entitlementUpdate struct {
	AccountID string
	State     entitlementState
	ExpiresAt time.Time
}

// entitlementStream 是 entitlement 更新的来源。生产实现由 ticket 25（StoreKit
// JWS entitlement）接入；此处以接口为 seam，测试注入 fake。subscribe 注册
// onUpdate 回调接收每次更新、onDown 回调在流断开（server offline）时触发；
// 返回的取消函数用于退订（Account 退出）。
type entitlementStream interface {
	subscribe(onUpdate func(entitlementUpdate), onDown func()) func()
}

// entitlementManager 跟踪 Account 的订阅授权状态，并把它暴露为 Relay 门禁输入。
// 它对 Pairing 与 LAN 无任何副作用：订阅变化只改这里的状态与 Relay 门禁。
type entitlementManager struct {
	mu       sync.Mutex
	stream   entitlementStream
	now      func() time.Time
	status   entitlementStatus
	cancel   func()
	timer    *time.Timer
	onChange func(entitlementStatus)
}

func newEntitlementManager(stream entitlementStream, now func() time.Time) *entitlementManager {
	if now == nil {
		now = time.Now
	}
	return &entitlementManager{
		stream: stream,
		now:    now,
		status: entitlementStatus{State: entitlementUnknown, Message: "订阅状态未知"},
	}
}

// start 订阅 entitlement stream。幂等：重复调用不重复订阅。stream 为 nil（生产
// 在 ticket 25 接入前）时保持 unknown。
func (m *entitlementManager) start() {
	m.mu.Lock()
	if m.cancel != nil || m.stream == nil {
		m.mu.Unlock()
		return
	}
	cancel := m.stream.subscribe(
		func(update entitlementUpdate) { m.apply(update) },
		func() { m.markOffline() },
	)
	m.cancel = cancel
	m.mu.Unlock()
}

// stop 取消订阅并回到 unknown。Account 退出时调用，绝不残留 active。
func (m *entitlementManager) stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	changed := m.resetUnknownLocked()
	next := m.status
	onChange := m.onChange
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if changed && onChange != nil {
		onChange(next)
	}
}

// resetUnknownLocked 在锁内把状态重置为 unknown 并停止到期计时器，返回是否需要通知。
// stop（Account 退出）与 markOffline（server offline）共用，保证两者语义一致。
func (m *entitlementManager) resetUnknownLocked() bool {
	m.stopTimerLocked()
	old := m.status
	changed := old.State != entitlementUnknown || old.AccountID != "" || old.RelayAllowed
	m.status = entitlementStatus{State: entitlementUnknown, Message: "订阅状态未知"}
	return changed
}

// current 返回当前订阅状态，并在读取时对 active/grace 做过期重算。
func (m *entitlementManager) current() entitlementStatus {
	m.recheck()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// relayAllowed 报告 Relay 是否被订阅授权放行。
func (m *entitlementManager) relayAllowed() bool {
	return m.current().State.allowsRelay()
}

// apply 应用一次 stream 更新并推进状态机，返回更新后的状态。
func (m *entitlementManager) apply(update entitlementUpdate) entitlementStatus {
	m.mu.Lock()
	status := m.resolveLocked(update)
	m.stopTimerLocked()
	if status.RelayAllowed && !status.ExpiresAt.IsZero() {
		m.scheduleExpiryLocked(status.ExpiresAt)
	}
	m.status = status
	onChange := m.onChange
	m.mu.Unlock()
	if onChange != nil {
		onChange(status)
	}
	return status
}

// markOffline 在 stream 断开（server offline）时把状态重置为 unknown，绝不残留
// active 造成公网误报。
func (m *entitlementManager) markOffline() {
	m.mu.Lock()
	changed := m.resetUnknownLocked()
	next := m.status
	onChange := m.onChange
	m.mu.Unlock()
	if changed && onChange != nil {
		onChange(next)
	}
}

// resolveLocked 把一次 stream 更新解析为本地状态：active/grace 但 ExpiresAt 已过
// 立即降级为 expired，避免误报 active；非法 State 归入 unknown。ExpiresAt 仅对
// active/grace 保留，expired/revoked/unknown 一律清零，避免状态与过期时间不自洽。
func (m *entitlementManager) resolveLocked(update entitlementUpdate) entitlementStatus {
	status := entitlementStatus{
		State:     update.State,
		AccountID: update.AccountID,
	}
	switch update.State {
	case entitlementActive, entitlementGrace, entitlementExpired, entitlementRevoked:
	default:
		status.State = entitlementUnknown
	}
	if status.State.allowsRelay() {
		status.ExpiresAt = update.ExpiresAt
		if !status.ExpiresAt.IsZero() && !m.now().Before(status.ExpiresAt) {
			status.State = entitlementExpired
			status.ExpiresAt = time.Time{}
		}
	}
	status.RelayAllowed = status.State.allowsRelay()
	status.Message = describeEntitlement(status.State)
	return status
}

// recheck 对 active/grace 做惰性过期重算，越过 ExpiresAt 则降级为 expired。
func (m *entitlementManager) recheck() {
	m.mu.Lock()
	status := m.status
	changed := false
	if status.RelayAllowed && !status.ExpiresAt.IsZero() && !m.now().Before(status.ExpiresAt) {
		m.stopTimerLocked()
		status = entitlementStatus{State: entitlementExpired, AccountID: status.AccountID, Message: "订阅已过期"}
		m.status = status
		changed = true
	}
	onChange := m.onChange
	m.mu.Unlock()
	if changed && onChange != nil {
		onChange(status)
	}
}

// scheduleExpiryLocked 在到期时刻安排一次主动降级，确保前端无需轮询也能收到
// expired 事件；越过 ExpiresAt 后由 recheck 完成实际降级。
func (m *entitlementManager) scheduleExpiryLocked(at time.Time) {
	d := at.Sub(m.now())
	if d <= 0 {
		return
	}
	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = time.AfterFunc(d, m.recheck)
}

func (m *entitlementManager) stopTimerLocked() {
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
}

func describeEntitlement(state entitlementState) string {
	switch state {
	case entitlementActive:
		return "订阅已生效"
	case entitlementGrace:
		return "订阅处于宽限期"
	case entitlementExpired:
		return "订阅已过期"
	case entitlementRevoked:
		return "订阅已被撤销"
	default:
		return "订阅状态未知"
	}
}

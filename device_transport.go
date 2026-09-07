package main

import (
	"sort"
	"sync"
	"time"
)

// deviceTransport 描述一个活动 Device 当前使用的传输路径：LAN 直连或 Relay 转发。
type deviceTransport string

const (
	deviceTransportLAN   deviceTransport = "lan"
	deviceTransportRelay deviceTransport = "relay"
)

// deviceActivity 描述一个 Device 最近一次活动的传输路径与时间。
type deviceActivity struct {
	DeviceID   string          `json:"deviceId"`
	Name       string          `json:"name"`
	Transport  deviceTransport `json:"transport"`
	LastActive time.Time       `json:"lastActive"`
}

// deviceActivityTTL 界定"活动"的窗口：超过该时长无活动的 Device 不再列入活动列表。
const deviceActivityTTL = 2 * time.Minute

// deviceTransportTracker 汇总 LAN 与 Relay 两条路径上的 Device 活动，暴露
// "哪个 Device 正在用哪条路径"的单一视图。它把 Relay 连接本身（由 relayHost
// 负责）与 Device 级别的传输活动分开，因此 Relay 在线绝不会被当作某个 Device
// 已连接。
type deviceTransportTracker struct {
	mu       sync.Mutex
	now      func() time.Time
	ttl      time.Duration
	activity map[string]deviceActivity
	onChange func([]deviceActivity)
}

func newDeviceTransportTracker(now func() time.Time) *deviceTransportTracker {
	return &deviceTransportTracker{
		now:      now,
		ttl:      deviceActivityTTL,
		activity: make(map[string]deviceActivity),
	}
}

// mark 记录一次 Device 活动。transport 变化（新进入，或 LAN↔Relay 切换）时触发
// onChange 快照；同一路径的重复活动只刷新 LastActive，不触发回调。
func (t *deviceTransportTracker) mark(activity deviceActivity) {
	if activity.DeviceID == "" {
		return
	}
	if activity.Name == "" {
		activity.Name = "Device"
	}
	activity.LastActive = t.now()
	t.mu.Lock()
	prev, existed := t.activity[activity.DeviceID]
	changed := !existed || prev.Transport != activity.Transport || prev.Name != activity.Name
	t.activity[activity.DeviceID] = activity
	var snapshot []deviceActivity
	if changed {
		snapshot = t.snapshotLocked()
	}
	onChange := t.onChange
	t.mu.Unlock()
	if changed && onChange != nil {
		onChange(snapshot)
	}
}

// activeDevices 返回 TTL 窗口内的活动 Device，按 DeviceID 稳定排序。
func (t *deviceTransportTracker) activeDevices() []deviceActivity {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}

func (t *deviceTransportTracker) snapshotLocked() []deviceActivity {
	cutoff := t.now().Add(-t.ttl)
	list := make([]deviceActivity, 0, len(t.activity))
	for id, a := range t.activity {
		if a.LastActive.Before(cutoff) {
			// 顺带清理过期条目，避免长跑时活动 map 无限增长。
			delete(t.activity, id)
			continue
		}
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].DeviceID < list[j].DeviceID })
	return list
}

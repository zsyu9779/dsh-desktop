package main

import (
	"sync"
	"testing"
	"time"
)

type fakeDeviceClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeDeviceClock() *fakeDeviceClock { return &fakeDeviceClock{t: time.Unix(1700000000, 0)} }

func (c *fakeDeviceClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeDeviceClock) advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func TestDeviceTransportTrackerReportsPerDeviceTransport(t *testing.T) {
	clock := newFakeDeviceClock()
	tracker := newDeviceTransportTracker(clock.now)
	tracker.mark(deviceActivity{DeviceID: "device-b", Name: "B", Transport: deviceTransportRelay})
	tracker.mark(deviceActivity{DeviceID: "device-a", Name: "A", Transport: deviceTransportLAN})

	active := tracker.activeDevices()
	if len(active) != 2 {
		t.Fatalf("active devices = %d, want 2", len(active))
	}
	if active[0].DeviceID != "device-a" || active[0].Transport != deviceTransportLAN {
		t.Fatalf("active[0] = %+v, want device-a/LAN", active[0])
	}
	if active[1].DeviceID != "device-b" || active[1].Transport != deviceTransportRelay {
		t.Fatalf("active[1] = %+v, want device-b/Relay", active[1])
	}
}

func TestDeviceTransportTrackerEmitsOnlyOnTransportChange(t *testing.T) {
	clock := newFakeDeviceClock()
	tracker := newDeviceTransportTracker(clock.now)
	var snapshots [][]deviceActivity
	tracker.onChange = func(list []deviceActivity) { snapshots = append(snapshots, list) }

	tracker.mark(deviceActivity{DeviceID: "device-a", Name: "A", Transport: deviceTransportLAN})   // 进入 → emit
	tracker.mark(deviceActivity{DeviceID: "device-a", Name: "A", Transport: deviceTransportLAN})   // 同路径 → 不 emit
	tracker.mark(deviceActivity{DeviceID: "device-a", Name: "A", Transport: deviceTransportRelay}) // 切换 → emit
	tracker.mark(deviceActivity{DeviceID: "device-a", Name: "A", Transport: deviceTransportLAN})   // 切回 → emit

	if len(snapshots) != 3 {
		t.Fatalf("onChange fired %d times, want 3", len(snapshots))
	}
	if snapshots[1][0].Transport != deviceTransportRelay {
		t.Fatalf("second snapshot transport = %q, want relay", snapshots[1][0].Transport)
	}
	if snapshots[2][0].Transport != deviceTransportLAN {
		t.Fatalf("third snapshot transport = %q, want lan", snapshots[2][0].Transport)
	}
}

func TestDeviceTransportTrackerExpiresIdleDevices(t *testing.T) {
	clock := newFakeDeviceClock()
	tracker := newDeviceTransportTracker(clock.now)
	tracker.mark(deviceActivity{DeviceID: "device-a", Name: "A", Transport: deviceTransportLAN})
	clock.advance(deviceActivityTTL + time.Second)

	if active := tracker.activeDevices(); len(active) != 0 {
		t.Fatalf("active devices after TTL = %d, want 0", len(active))
	}
}

func TestDeviceTransportTrackerRefreshKeepsDeviceActive(t *testing.T) {
	clock := newFakeDeviceClock()
	tracker := newDeviceTransportTracker(clock.now)
	tracker.mark(deviceActivity{DeviceID: "device-a", Name: "A", Transport: deviceTransportLAN})
	clock.advance(deviceActivityTTL - time.Second)
	tracker.mark(deviceActivity{DeviceID: "device-a", Name: "A", Transport: deviceTransportLAN}) // 刷新 LastActive
	clock.advance(time.Second)

	if active := tracker.activeDevices(); len(active) != 1 {
		t.Fatalf("active devices = %d, want 1", len(active))
	}
}

func TestAppActiveDevicesReturnsTransportSnapshot(t *testing.T) {
	app := &App{transport: newDeviceTransportTracker(time.Now)}
	app.transport.mark(deviceActivity{DeviceID: "device-a", Name: "A", Transport: deviceTransportLAN})
	if got := app.ActiveDevices(); len(got) != 1 {
		t.Fatalf("ActiveDevices = %d, want 1", len(got))
	}
}

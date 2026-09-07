package main

import (
	"sync"
	"testing"
	"time"
)

// fakeEntitlementStream 是可编程的 entitlement stream：测试通过 push/down 驱动
// 状态转换，覆盖全部 entitlement 状态与 server offline。
type fakeEntitlementStream struct {
	mu       sync.Mutex
	onUpdate func(entitlementUpdate)
	onDown   func()
	subs     int
}

func (s *fakeEntitlementStream) subscribe(onUpdate func(entitlementUpdate), onDown func()) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onUpdate = onUpdate
	s.onDown = onDown
	s.subs++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.onUpdate = nil
		s.onDown = nil
	}
}

func (s *fakeEntitlementStream) push(update entitlementUpdate) {
	s.mu.Lock()
	fn := s.onUpdate
	s.mu.Unlock()
	if fn != nil {
		fn(update)
	}
}

func (s *fakeEntitlementStream) down() {
	s.mu.Lock()
	fn := s.onDown
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (s *fakeEntitlementStream) subscribeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.subs
}

// fakeEntitlementClock 提供可前进的时钟，用于确定性验证状态过期降级。
type fakeEntitlementClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeEntitlementClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeEntitlementClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func TestEntitlementStateRelayGating(t *testing.T) {
	tests := []struct {
		state  entitlementState
		allow  bool
		active bool
	}{
		{entitlementActive, true, true},
		{entitlementGrace, true, true},
		{entitlementExpired, false, false},
		{entitlementRevoked, false, false},
		{entitlementUnknown, false, false},
	}
	for _, tt := range tests {
		if got := tt.state.allowsRelay(); got != tt.allow {
			t.Fatalf("%s allowsRelay = %v, want %v", tt.state, got, tt.allow)
		}
	}
}

func TestEntitlementManagerStartsUnknown(t *testing.T) {
	manager := newEntitlementManager(&fakeEntitlementStream{}, nil)
	status := manager.current()
	if status.State != entitlementUnknown {
		t.Fatalf("initial state = %q, want unknown", status.State)
	}
	if status.RelayAllowed {
		t.Fatal("unknown must not allow Relay")
	}
	if manager.relayAllowed() {
		t.Fatal("relayAllowed() = true, want false for unknown")
	}
}

func TestEntitlementManagerResolvesStreamUpdates(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	clock := &fakeEntitlementClock{t: now}
	manager := newEntitlementManager(&fakeEntitlementStream{}, clock.now)

	tests := []struct {
		name    string
		update  entitlementUpdate
		want    entitlementState
		allowed bool
	}{
		{name: "active", update: entitlementUpdate{AccountID: "account-a", State: entitlementActive, ExpiresAt: now.Add(time.Hour)}, want: entitlementActive, allowed: true},
		{name: "grace", update: entitlementUpdate{AccountID: "account-a", State: entitlementGrace, ExpiresAt: now.Add(time.Hour)}, want: entitlementGrace, allowed: true},
		{name: "expired", update: entitlementUpdate{AccountID: "account-a", State: entitlementExpired}, want: entitlementExpired, allowed: false},
		{name: "revoked", update: entitlementUpdate{AccountID: "account-a", State: entitlementRevoked}, want: entitlementRevoked, allowed: false},
		{name: "invalid becomes unknown", update: entitlementUpdate{AccountID: "account-a", State: "bogus"}, want: entitlementUnknown, allowed: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := manager.apply(tt.update)
			if status.State != tt.want {
				t.Fatalf("apply state = %q, want %q", status.State, tt.want)
			}
			if status.RelayAllowed != tt.allowed {
				t.Fatalf("apply RelayAllowed = %v, want %v", status.RelayAllowed, tt.allowed)
			}
			if status.AccountID != "account-a" {
				t.Fatalf("apply AccountID = %q, want account-a", status.AccountID)
			}
		})
	}
}

func TestEntitlementExpiredUpdateDoesNotReportActive(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	manager := newEntitlementManager(&fakeEntitlementStream{}, func() time.Time { return now })

	// active 但 ExpiresAt 已过：立即降级为 expired，绝不误报 active。
	status := manager.apply(entitlementUpdate{AccountID: "account-a", State: entitlementActive, ExpiresAt: now.Add(-time.Minute)})
	if status.State != entitlementExpired {
		t.Fatalf("state = %q, want expired", status.State)
	}
	if status.RelayAllowed {
		t.Fatal("expired must not allow Relay")
	}
	if manager.relayAllowed() {
		t.Fatal("relayAllowed() = true, want false for expired")
	}
}

func TestEntitlementStatusExpiryDegradesOnRead(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	clock := &fakeEntitlementClock{t: now}
	manager := newEntitlementManager(&fakeEntitlementStream{}, clock.now)
	manager.apply(entitlementUpdate{AccountID: "account-a", State: entitlementActive, ExpiresAt: now.Add(time.Hour)})

	if got := manager.current().State; got != entitlementActive {
		t.Fatalf("before expiry state = %q, want active", got)
	}

	// 越过 ExpiresAt 后，读取时降级为 expired，不再误报 active。
	clock.advance(2 * time.Hour)
	if got := manager.current().State; got != entitlementExpired {
		t.Fatalf("after expiry state = %q, want expired", got)
	}
	if manager.relayAllowed() {
		t.Fatal("relayAllowed() = true, want false after expiry")
	}
}

func TestEntitlementGraceExpiryDegradesToExpired(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	clock := &fakeEntitlementClock{t: now}
	manager := newEntitlementManager(&fakeEntitlementStream{}, clock.now)
	manager.apply(entitlementUpdate{AccountID: "account-a", State: entitlementGrace, ExpiresAt: now.Add(time.Hour)})

	if !manager.relayAllowed() {
		t.Fatal("grace must allow Relay")
	}
	clock.advance(2 * time.Hour)
	if got := manager.current().State; got != entitlementExpired {
		t.Fatalf("grace expiry state = %q, want expired", got)
	}
}

func TestEntitlementServerOfflineResetsUnknown(t *testing.T) {
	stream := &fakeEntitlementStream{}
	manager := newEntitlementManager(stream, nil)
	manager.start()
	manager.apply(entitlementUpdate{AccountID: "account-a", State: entitlementActive, ExpiresAt: time.Now().Add(time.Hour)})
	if got := manager.current().State; got != entitlementActive {
		t.Fatalf("after update state = %q, want active", got)
	}

	stream.down()

	if got := manager.current().State; got != entitlementUnknown {
		t.Fatalf("after offline state = %q, want unknown", got)
	}
	if manager.relayAllowed() {
		t.Fatal("offline must not allow Relay")
	}
}

func TestEntitlementStopResetsUnknown(t *testing.T) {
	stream := &fakeEntitlementStream{}
	manager := newEntitlementManager(stream, nil)
	manager.start()
	manager.apply(entitlementUpdate{AccountID: "account-a", State: entitlementActive, ExpiresAt: time.Now().Add(time.Hour)})

	manager.stop()

	status := manager.current()
	if status.State != entitlementUnknown || status.AccountID != "" {
		t.Fatalf("after stop status = %+v, want unknown without AccountID", status)
	}
	if manager.relayAllowed() {
		t.Fatal("stop must not allow Relay")
	}

	// stop 后 stream 再推更新也不应生效（已退订）。
	stream.push(entitlementUpdate{AccountID: "account-a", State: entitlementActive, ExpiresAt: time.Now().Add(time.Hour)})
	if got := manager.current().State; got != entitlementUnknown {
		t.Fatalf("update after stop state = %q, want unknown", got)
	}
}

func TestEntitlementRevokedThenReactivated(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	manager := newEntitlementManager(&fakeEntitlementStream{}, func() time.Time { return now })
	manager.apply(entitlementUpdate{AccountID: "account-a", State: entitlementActive, ExpiresAt: now.Add(time.Hour)})
	manager.apply(entitlementUpdate{AccountID: "account-a", State: entitlementRevoked})
	if manager.relayAllowed() {
		t.Fatal("revoked must not allow Relay")
	}

	// 重新订阅：revoked 可回到 active。
	status := manager.apply(entitlementUpdate{AccountID: "account-a", State: entitlementActive, ExpiresAt: now.Add(time.Hour)})
	if status.State != entitlementActive || !manager.relayAllowed() {
		t.Fatalf("reactivate = %+v, want active with Relay allowed", status)
	}
}

func TestEntitlementStartIsIdempotent(t *testing.T) {
	stream := &fakeEntitlementStream{}
	manager := newEntitlementManager(stream, nil)
	manager.start()
	manager.start()
	if n := stream.subscribeCount(); n != 1 {
		t.Fatalf("subscribe count = %d, want 1", n)
	}
}

func TestEntitlementTimerEmitsExpiredProactively(t *testing.T) {
	stream := &fakeEntitlementStream{}
	manager := newEntitlementManager(stream, nil)
	changed := make(chan entitlementStatus, 8)
	manager.onChange = func(s entitlementStatus) { changed <- s }
	manager.start()

	expiresAt := time.Now().Add(30 * time.Millisecond)
	manager.apply(entitlementUpdate{AccountID: "account-a", State: entitlementActive, ExpiresAt: expiresAt})
	<-changed // active

	select {
	case status := <-changed:
		if status.State != entitlementExpired {
			t.Fatalf("proactive expiry state = %q, want expired", status.State)
		}
		if status.RelayAllowed {
			t.Fatal("proactive expiry must not allow Relay")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("entitlement timer did not emit expired within 2s")
	}
}

func TestEntitlementTransitionMatrix(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	manager := newEntitlementManager(&fakeEntitlementStream{}, func() time.Time { return now })

	// 扁平状态机：依次应用每个目标状态，覆盖全部 5 个状态与两两转换方向。
	for _, target := range []entitlementState{entitlementActive, entitlementGrace, entitlementExpired, entitlementRevoked, entitlementUnknown} {
		update := entitlementUpdate{AccountID: "account-a", State: target}
		if target == entitlementActive || target == entitlementGrace {
			update.ExpiresAt = now.Add(time.Hour)
		}
		status := manager.apply(update)
		if status.State != target {
			t.Fatalf("apply(%q) state = %q, want %q", target, status.State, target)
		}
		if status.RelayAllowed != target.allowsRelay() {
			t.Fatalf("apply(%q) RelayAllowed = %v, want %v", target, status.RelayAllowed, target.allowsRelay())
		}
	}
}

func TestEntitlementChangeCallbackFiresForAllTransitions(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	manager := newEntitlementManager(&fakeEntitlementStream{}, func() time.Time { return now })
	events := make(chan entitlementStatus, 16)
	manager.onChange = func(s entitlementStatus) { events <- s }

	states := []entitlementState{entitlementActive, entitlementGrace, entitlementExpired, entitlementRevoked}
	for _, st := range states {
		update := entitlementUpdate{AccountID: "account-a", State: st}
		if st == entitlementActive || st == entitlementGrace {
			update.ExpiresAt = now.Add(time.Hour)
		}
		manager.apply(update)
	}

	// 每次状态变化都必须推一次 onChange（即前端 entitlement 事件）。
	for _, want := range states {
		select {
		case got := <-events:
			if got.State != want {
				t.Fatalf("change event state = %q, want %q", got.State, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing change event for state %q", want)
		}
	}
}

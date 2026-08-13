//go:build smoke

package main

import (
	"testing"
	"time"
)

// TestDSHManagerReachesReady is an end-to-end smoke test of the launcher logic
// without the GUI: it spawns the real `npx dsh web` process and waits for the
// local web UI to come up. Skip with -short or DSH_SMOKE_SKIP=1.
func TestDSHManagerReachesReady(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration smoke test in short mode")
	}

	m := newDSHManager(&App{}) // nil ctx: no events/dialogs, pure process lifecycle
	m.start()

	deadline := time.Now().Add(200 * time.Second)
	for time.Now().Before(deadline) {
		s := m.current()
		switch s.State {
		case "ready":
			t.Logf("READY at %s (port %d)", s.URL, s.Port)
			m.stop()
			return
		case "error", "exited":
			m.stop()
			t.Fatalf("manager reached state %q: %s\n--- logs ---\n%s", s.State, s.Message, m.logsString())
		}
		time.Sleep(1 * time.Second)
	}

	m.stop()
	t.Fatalf("timed out waiting for ready; last state=%q\n--- logs ---\n%s", m.current().State, m.logsString())
}

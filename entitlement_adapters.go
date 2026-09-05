package main

import (
	"context"
	"sync"
	"time"
)

type pollingEntitlementStream struct {
	account  *accountManager
	server   *httpAccountServer
	interval time.Duration
}

func newPollingEntitlementStream(account *accountManager, server *httpAccountServer, interval time.Duration) *pollingEntitlementStream {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &pollingEntitlementStream{account: account, server: server, interval: interval}
}

func (s *pollingEntitlementStream) subscribe(onUpdate func(entitlementUpdate), onDown func()) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		poll := func() {
			credential, err := s.account.currentCredential()
			if err != nil {
				onDown()
				return
			}
			requestCtx, requestCancel := context.WithTimeout(ctx, 15*time.Second)
			update, err := s.server.subscriptionStatus(requestCtx, credential.AccessToken)
			requestCancel()
			if err != nil {
				onDown()
				return
			}
			onUpdate(update)
		}
		poll()
		for {
			select {
			case <-ticker.C:
				poll()
			case <-ctx.Done():
				return
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(cancel) }
}

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostCanSignInToAccount(t *testing.T) {
	store := newMemorySecretStore()
	server := &fakeAccountServer{
		credential: accountCredential{
			AccountID:   "account-a",
			AccessToken: "access-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	manager := newAccountManager(
		fakeAccountAuthorizer{token: "apple-identity-token"},
		server,
		store,
	)
	app := &App{account: manager}

	status := app.SignInAccount()

	if status.State != accountStateSignedIn {
		t.Fatalf("state = %q, want %q (message: %s)", status.State, accountStateSignedIn, status.Message)
	}
	if status.AccountID != "account-a" {
		t.Fatalf("AccountID = %q, want account-a", status.AccountID)
	}
	if status.HostID == "" {
		t.Fatal("HostID is empty")
	}
	if server.authenticatedToken != "apple-identity-token" {
		t.Fatalf("authenticated token = %q, want apple-identity-token", server.authenticatedToken)
	}
	if server.registration.HostID != status.HostID {
		t.Fatalf("registered HostID = %q, status HostID = %q", server.registration.HostID, status.HostID)
	}
	if len(server.registration.IdentityPublicKey) == 0 {
		t.Fatal("registered identity public key is empty")
	}
	if current := app.AccountStatus(); current != status {
		t.Fatalf("AccountStatus = %+v, want %+v", current, status)
	}
}

func TestRestoredCredentialRejectedAfterServerRestartBecomesSignedOut(t *testing.T) {
	store := newMemorySecretStore()
	credential := validAccountCredential()
	encoded, err := json.Marshal(credential)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.set(accountCredentialSecret, string(encoded)); err != nil {
		t.Fatal(err)
	}
	identity := hostAccountIdentity{KeyType: identityKeyTypeX25519, HostID: "host-restored", PrivateKey: base64.RawStdEncoding.EncodeToString(make([]byte, 32)), RegisteredAccounts: map[string]bool{credential.AccountID: true}}
	identityJSON, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.set(accountIdentitySecret, string(identityJSON)); err != nil {
		t.Fatal(err)
	}
	server := &validatingFakeAccountServer{
		fakeAccountServer: fakeAccountServer{credential: credential},
		validationErr:     errAccountRejected,
	}
	manager := newAccountManager(fakeAccountAuthorizer{}, server, store)

	status := manager.validateRestoredCredential(context.Background())

	if status.State != accountStateSignedOut || !status.Retryable {
		t.Fatalf("status = %+v", status)
	}
	if _, err := store.get(accountCredentialSecret); !errors.Is(err, errAccountSecretNotFound) {
		t.Fatalf("credential remained after rejection: %v", err)
	}
}

func TestRestoredCredentialNetworkFailureWaitsForValidationWithoutDeletingSecret(t *testing.T) {
	store := newMemorySecretStore()
	credential := validAccountCredential()
	encoded, _ := json.Marshal(credential)
	_ = store.set(accountCredentialSecret, string(encoded))
	identity := hostAccountIdentity{KeyType: identityKeyTypeX25519, HostID: "host-restored", PrivateKey: base64.RawStdEncoding.EncodeToString(make([]byte, 32)), RegisteredAccounts: map[string]bool{credential.AccountID: true}}
	identityJSON, _ := json.Marshal(identity)
	_ = store.set(accountIdentitySecret, string(identityJSON))
	server := &validatingFakeAccountServer{fakeAccountServer: fakeAccountServer{credential: credential}, validationErr: errors.New("offline")}
	manager := newAccountManager(fakeAccountAuthorizer{}, server, store)

	if status := manager.validateRestoredCredential(context.Background()); status.State != accountStateValidating {
		t.Fatalf("status = %+v, want pending network validation", status)
	}
	if _, err := store.get(accountCredentialSecret); err != nil {
		t.Fatalf("transient validation deleted credential: %v", err)
	}
	server.validationErr = nil
	if status := manager.validateRestoredCredential(context.Background()); status.State != accountStateSignedIn {
		t.Fatalf("revalidated status = %+v", status)
	}
}

func TestHostIdentityIsReusedWithoutRepeatedRegistration(t *testing.T) {
	store := newMemorySecretStore()
	server := &fakeAccountServer{credential: validAccountCredential()}
	first := newAccountManager(fakeAccountAuthorizer{token: "first-token"}, server, store)
	firstStatus := first.signIn(context.Background())
	if firstStatus.State != accountStateSignedIn {
		t.Fatalf("first sign-in failed: %+v", firstStatus)
	}
	first.signOut()

	restarted := newAccountManager(fakeAccountAuthorizer{token: "second-token"}, server, store)
	secondStatus := restarted.signIn(context.Background())

	if secondStatus.HostID != firstStatus.HostID {
		t.Fatalf("restarted HostID = %q, want %q", secondStatus.HostID, firstStatus.HostID)
	}
	if server.registrationCount != 1 {
		t.Fatalf("Host registration count = %d, want 1", server.registrationCount)
	}
}

func TestRestartRestoresAccountCredentialWithoutServerRegistration(t *testing.T) {
	store := newMemorySecretStore()
	server := &fakeAccountServer{credential: validAccountCredential()}
	first := newAccountManager(fakeAccountAuthorizer{token: "token"}, server, store)
	want := first.signIn(context.Background())

	restarted := newAccountManager(fakeAccountAuthorizer{}, server, store)
	got := restarted.currentStatus()

	if got.State != accountStateSignedIn || got.AccountID != want.AccountID || got.HostID != want.HostID {
		t.Fatalf("restored status = %+v, want signed-in Account %q Host %q", got, want.AccountID, want.HostID)
	}
	if server.registrationCount != 1 {
		t.Fatalf("Host registration count after restart = %d, want 1", server.registrationCount)
	}
}

func TestSignOutClearsOnlyAccountCredential(t *testing.T) {
	lanDir := t.TempDir()
	lanData := filepath.Join(lanDir, "devices.json")
	if err := os.WriteFile(lanData, []byte("paired-device"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newMemorySecretStore()
	manager := newAccountManager(
		fakeAccountAuthorizer{token: "token"},
		&fakeAccountServer{credential: validAccountCredential()},
		store,
	)
	if got := manager.signIn(context.Background()); got.State != accountStateSignedIn {
		t.Fatalf("sign-in failed: %+v", got)
	}

	status := manager.signOut()

	if status.State != accountStateSignedOut {
		t.Fatalf("state = %q, want signed-out", status.State)
	}
	if _, ok := store.values[accountCredentialSecret]; ok {
		t.Fatal("Account credential remains in secure storage")
	}
	if _, ok := store.values[accountIdentitySecret]; !ok {
		t.Fatal("Host identity was deleted on sign-out")
	}
	if got, err := os.ReadFile(lanData); err != nil || string(got) != "paired-device" {
		t.Fatalf("LAN data changed on sign-out: data=%q err=%v", got, err)
	}
}

func TestAccountSignInFailuresAreUnderstandableAndRetryable(t *testing.T) {
	tests := []struct {
		name          string
		authorizerErr error
		authenticate  error
		register      error
		wantState     accountState
		wantMessage   string
	}{
		{name: "Owner 取消", authorizerErr: errAccountAuthorizationCanceled, wantState: accountStateCanceled, wantMessage: "已取消"},
		{name: "授权过期", authorizerErr: errAccountAuthorizationExpired, wantState: accountStateExpired, wantMessage: "已过期"},
		{name: "浏览器网络失败", authorizerErr: temporaryNetworkError{}, wantState: accountStateNetworkError, wantMessage: "网络"},
		{name: "Account server 网络失败", authenticate: temporaryNetworkError{}, wantState: accountStateNetworkError, wantMessage: "网络"},
		{name: "Account server 拒绝", authenticate: errAccountRejected, wantState: accountStateRejected, wantMessage: "拒绝"},
		{name: "Host 登记被拒绝", register: errAccountRejected, wantState: accountStateRejected, wantMessage: "拒绝"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &fakeAccountServer{
				credential:      validAccountCredential(),
				authenticateErr: tt.authenticate,
				registerErr:     tt.register,
			}
			manager := newAccountManager(
				fakeAccountAuthorizer{token: "token", err: tt.authorizerErr},
				server,
				newMemorySecretStore(),
			)

			status := manager.signIn(context.Background())

			if status.State != tt.wantState {
				t.Fatalf("state = %q, want %q (message: %s)", status.State, tt.wantState, status.Message)
			}
			if !status.Retryable {
				t.Fatal("failure is not retryable")
			}
			if !strings.Contains(status.Message, tt.wantMessage) {
				t.Fatalf("message = %q, want it to contain %q", status.Message, tt.wantMessage)
			}
		})
	}
}

func TestAccountSignInCanRetryAfterCancellation(t *testing.T) {
	authorizer := &sequenceAccountAuthorizer{
		results: []authorizationResult{
			{err: errAccountAuthorizationCanceled},
			{token: "apple-token"},
		},
	}
	manager := newAccountManager(authorizer, &fakeAccountServer{credential: validAccountCredential()}, newMemorySecretStore())
	if got := manager.signIn(context.Background()); got.State != accountStateCanceled {
		t.Fatalf("first state = %q, want canceled", got.State)
	}
	if got := manager.signIn(context.Background()); got.State != accountStateSignedIn {
		t.Fatalf("retry state = %q, want signed-in (message: %s)", got.State, got.Message)
	}
}

func TestHostCanCompleteBrowserCallbackSignIn(t *testing.T) {
	var openedURL string
	authorizer := newBrowserAccountAuthorizer("https://accounts.example.test", func(rawURL string) error {
		openedURL = rawURL
		authorizationURL, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		callbackURL, err := url.Parse(authorizationURL.Query().Get("redirect_uri"))
		if err != nil {
			return err
		}
		response, err := http.PostForm(callbackURL.String(), url.Values{
			"state":          {authorizationURL.Query().Get("state")},
			"identity_token": {"apple-callback-token"},
		})
		if err == nil {
			response.Body.Close()
		}
		return err
	})
	server := &fakeAccountServer{credential: validAccountCredential()}
	manager := newAccountManager(authorizer, server, newMemorySecretStore())

	status := manager.signIn(context.Background())

	if status.State != accountStateSignedIn {
		t.Fatalf("state = %q, want signed-in (message: %s)", status.State, status.Message)
	}
	if server.authenticatedToken != "apple-callback-token" {
		t.Fatalf("authenticated token = %q, want callback token", server.authenticatedToken)
	}
	if !strings.HasPrefix(openedURL, "https://accounts.example.test/v1/account/apple/authorize?") {
		t.Fatalf("opened URL = %q", openedURL)
	}
}

func TestHTTPAccountAdapterAuthenticatesAndRegistersHost(t *testing.T) {
	var authenticated bool
	var registered bool
	service := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/account/authenticate":
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode authentication request: %v", err)
			}
			if body["identityToken"] != "apple-token" {
				t.Errorf("identity token = %q, want apple-token", body["identityToken"])
			}
			authenticated = true
			_ = json.NewEncoder(response).Encode(validAccountCredential())
		case "/v1/account/hosts":
			if request.Header.Get("Authorization") != "Bearer access-token" {
				t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
			}
			var registration hostAccountRegistration
			if err := json.NewDecoder(request.Body).Decode(&registration); err != nil {
				t.Errorf("decode Host registration: %v", err)
			}
			if registration.HostID == "" || registration.IdentityPublicKey == "" {
				t.Errorf("incomplete Host registration: %+v", registration)
			}
			registered = true
			response.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(response, request)
		}
	}))
	defer service.Close()

	manager := newAccountManager(
		fakeAccountAuthorizer{token: "apple-token"},
		newHTTPAccountServer(service.URL),
		newMemorySecretStore(),
	)
	status := manager.signIn(context.Background())

	if status.State != accountStateSignedIn {
		t.Fatalf("state = %q, want signed-in (message: %s)", status.State, status.Message)
	}
	if !authenticated || !registered {
		t.Fatalf("authenticated=%t registered=%t, want both true", authenticated, registered)
	}
}

func TestAccountStatusShowsAuthorizationInProgress(t *testing.T) {
	authorizer := &blockingAccountAuthorizer{started: make(chan struct{}), release: make(chan struct{})}
	manager := newAccountManager(authorizer, &fakeAccountServer{credential: validAccountCredential()}, newMemorySecretStore())
	finished := make(chan accountStatus, 1)
	go func() { finished <- manager.signIn(context.Background()) }()
	<-authorizer.started

	status := manager.currentStatus()

	if status.State != accountStateAuthorizing {
		t.Fatalf("state = %q, want authorizing", status.State)
	}
	close(authorizer.release)
	if got := <-finished; got.State != accountStateSignedIn {
		t.Fatalf("finished state = %q, want signed-in", got.State)
	}
}

func validAccountCredential() accountCredential {
	return accountCredential{AccountID: "account-a", AccessToken: "access-token", ExpiresAt: time.Now().Add(time.Hour)}
}

type temporaryNetworkError struct{}

func (temporaryNetworkError) Error() string   { return "temporary network failure" }
func (temporaryNetworkError) Timeout() bool   { return false }
func (temporaryNetworkError) Temporary() bool { return true }

var _ net.Error = temporaryNetworkError{}

type authorizationResult struct {
	token string
	err   error
}

type sequenceAccountAuthorizer struct {
	results []authorizationResult
}

type blockingAccountAuthorizer struct {
	started chan struct{}
	release chan struct{}
}

func (a *blockingAccountAuthorizer) authorize(context.Context) (string, error) {
	close(a.started)
	<-a.release
	return "apple-token", nil
}

func (a *sequenceAccountAuthorizer) authorize(context.Context) (string, error) {
	result := a.results[0]
	a.results = a.results[1:]
	return result.token, result.err
}

type fakeAccountAuthorizer struct {
	token string
	err   error
}

func (f fakeAccountAuthorizer) authorize(context.Context) (string, error) {
	return f.token, f.err
}

type fakeAccountServer struct {
	credential         accountCredential
	authenticateErr    error
	registerErr        error
	migrateErr         error
	authenticatedToken string
	registration       hostAccountRegistration
	registrationCount  int
	migration          hostAccountMigration
	migrateCount       int
}

type validatingFakeAccountServer struct {
	fakeAccountServer
	validationErr error
}

func (f *validatingFakeAccountServer) validateCredential(context.Context, string) error {
	return f.validationErr
}

func (f *fakeAccountServer) authenticate(_ context.Context, identityToken string) (accountCredential, error) {
	f.authenticatedToken = identityToken
	return f.credential, f.authenticateErr
}

func (f *fakeAccountServer) registerHost(_ context.Context, _ string, registration hostAccountRegistration) error {
	f.registration = registration
	f.registrationCount++
	return f.registerErr
}

func (f *fakeAccountServer) migrateHost(_ context.Context, _ string, migration hostAccountMigration) error {
	f.migration = migration
	f.migrateCount++
	return f.migrateErr
}

type memorySecretStore struct {
	values map[string]string
}

func newMemorySecretStore() *memorySecretStore {
	return &memorySecretStore{values: make(map[string]string)}
}

func (s *memorySecretStore) get(key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", errAccountSecretNotFound
	}
	return value, nil
}

func (s *memorySecretStore) set(key, value string) error {
	s.values[key] = value
	return nil
}

func (s *memorySecretStore) delete(key string) error {
	if _, ok := s.values[key]; !ok {
		return errAccountSecretNotFound
	}
	delete(s.values, key)
	return nil
}

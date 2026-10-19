package beatport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type renewalWaitContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (ctx *renewalWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.entered) })
	return ctx.Context.Done()
}

func TestConcurrentAuthFailuresShareRenewalResult(t *testing.T) {
	for _, tc := range []struct{ recovery, timeout bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		t.Run(fmt.Sprintf("unauthorized=%v/timeout=%v", tc.recovery, tc.timeout), func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var refreshes, logins atomic.Int32
			base := authTestClient(t, func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, tokenEndpoint) {
					if refreshes.Add(1) == 1 {
						close(started)
						<-release
					}
					if tc.timeout {
						return nil, context.DeadlineExceeded
					}
					return authTestResponse(req, 401, `{"detail":"refresh rejected"}`), nil
				}
				logins.Add(1)
				return authTestResponse(req, 401, `{"detail":"login rejected"}`), nil
			})
			if !tc.recovery {
				base.auth.Invalidate()
			}
			rejected := base.auth.snapshot()
			attempt := func(inst *Beatport) error {
				if tc.recovery {
					return inst.auth.recoverUnauthorized(inst, rejected)
				}
				return inst.auth.Check(inst)
			}
			const waiters = 8
			results := make(chan error, waiters+1)
			go func() { results <- attempt(base) }()
			<-started
			for i := 0; i < waiters; i++ {
				ctx := &renewalWaitContext{Context: context.Background(), entered: make(chan struct{})}
				bound := base.WithContext(ctx)
				go func() { results <- attempt(bound) }()
				<-ctx.entered
			}
			close(release)
			for i := 0; i <= waiters; i++ {
				err := <-results
				var apiErr *APIError
				if tc.timeout {
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Errorf("renewal error = %v, want shared transport timeout", err)
					}
				} else if !errors.As(err, &apiErr) || apiErr.Detail != "login rejected" {
					t.Errorf("renewal error = %v, want shared login rejection", err)
				}
			}
			wantLogins := int32(1)
			if tc.timeout {
				wantLogins = 0
			}
			if refreshes.Load() != 1 || logins.Load() != wantLogins {
				t.Fatalf("refresh/login calls = %d/%d, want 1/%d for overlapping callers", refreshes.Load(), logins.Load(), wantLogins)
			}
			// A later independent attempt must not be permanently cached as failed.
			_ = attempt(base)
			if refreshes.Load() != 2 || logins.Load() != 2*wantLogins {
				t.Fatalf("later attempt did not retry: refresh/login = %d/%d", refreshes.Load(), logins.Load())
			}
		})
	}
}

func TestCancelledRenewalDoesNotCancelAnotherClient(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	base := authTestClient(t, func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		return authTestResponse(req, 200, `{"access_token":"new-token","refresh_token":"new-refresh","expires_in":3600}`), nil
	})
	base.auth.Invalidate()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bound := base.WithContext(ctx)
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- bound.auth.Check(bound) }()
	<-started
	waitCtx := &renewalWaitContext{Context: context.Background(), entered: make(chan struct{})}
	waiter := base.WithContext(waitCtx)
	waiterDone := make(chan error, 1)
	go func() { waiterDone <- waiter.auth.Check(waiter) }()
	<-waitCtx.entered
	cancel()
	if err := <-ownerDone; !errors.Is(err, context.Canceled) {
		t.Errorf("owner error = %v, want cancellation", err)
	}
	if err := <-waiterDone; err != nil {
		t.Errorf("independent waiter failed after owner cancellation: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("auth calls = %d, want one canceled and one successful refresh", calls.Load())
	}
}

func TestRejectionOfPublishedTokenGetsItsOwnRecovery(t *testing.T) {
	var calls atomic.Int32
	base := authTestClient(t, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return authTestResponse(req, 200, `{"access_token":"refreshed-again","refresh_token":"new-refresh","expires_in":3600}`), nil
	})
	if err := base.auth.lockRefresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Model the interval after token publication but before cache/recovery completion.
	base.auth.mutex.Lock()
	issued := *base.auth.tokenPair
	issued.AccessToken = "just-issued"
	base.auth.tokenPair = &issued
	base.auth.mutex.Unlock()
	ctx := &renewalWaitContext{Context: context.Background(), entered: make(chan struct{})}
	bound := base.WithContext(ctx)
	done := make(chan error, 1)
	go func() { done <- bound.auth.recoverUnauthorized(bound, &issued) }()
	<-ctx.entered
	base.auth.mutex.Lock()
	base.auth.lastRenewal = &authRenewalResult{}
	base.auth.mutex.Unlock()
	base.auth.unlockRefresh()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || base.auth.snapshot().AccessToken != "refreshed-again" {
		t.Fatalf("rejected new token reused prior success: auth calls=%d, token=%q", calls.Load(), base.auth.snapshot().AccessToken)
	}
}

type authResponseBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *authResponseBody) Close() error {
	b.closed.Store(true)
	return nil
}

func authTestResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       &authResponseBody{Reader: strings.NewReader(body)},
		Request:    req,
	}
}

func authTestClient(t *testing.T, transport roundTripFunc) *Beatport {
	t.Helper()
	inst := authenticatedTestClient(&http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	})
	inst.auth.cacheFile = filepath.Join(t.TempDir(), "tokens.json")
	inst.auth.tokenPair.RefreshToken = "test-refresh"
	inst.auth.tokenPair.LoginID = inst.auth.loginId()
	return inst
}

func TestFetchUnauthorizedRecoveryIsBounded(t *testing.T) {
	var bodies []*authResponseBody
	catalogCalls, refreshCalls := 0, 0
	inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, tokenEndpoint) {
			refreshCalls++
			if !bodies[0].closed.Load() {
				t.Error("rejected body is still open during refresh")
			}
			return authTestResponse(req, 200, `{"access_token":"new-token","refresh_token":"new-refresh","expires_in":3600}`), nil
		}
		catalogCalls++
		status := http.StatusUnauthorized
		if catalogCalls > 2 {
			// Cap the broken recursive implementation so the regression terminates.
			status = http.StatusForbidden
		}
		res := authTestResponse(req, status, `{"detail":"catalog token rejected"}`)
		bodies = append(bodies, res.Body.(*authResponseBody))
		return res, nil
	})
	_, err := inst.fetch(http.MethodGet, "/catalog/tracks/42/", nil, "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 || apiErr.Detail != "catalog token rejected" || apiErr.Endpoint != "/catalog/tracks/42/" {
		t.Errorf("fetch() error = %#v, want catalog 401 with detail", err)
	}
	if catalogCalls != 2 || refreshCalls != 1 {
		t.Errorf("catalog/refresh calls = %d/%d, want 2/1", catalogCalls, refreshCalls)
	}
	for i, body := range bodies {
		if !body.closed.Load() {
			t.Errorf("rejected body %d was not closed", i)
		}
	}
}

func TestFetchUnauthorizedReplaysPostPayload(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, wantBody string
		payload                     interface{}
	}{
		{"json", "application/json", "{\"name\":\"synthetic\"}\n", map[string]string{"name": "synthetic"}},
		{"form", "application/x-www-form-urlencoded", "name=synthetic+value", map[string]string{"name": "synthetic value"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var rejected *authResponseBody
			inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, tokenEndpoint) {
					if !rejected.closed.Load() {
						t.Error("rejected body is still open during refresh")
					}
					return authTestResponse(req, 200, `{"access_token":"new-token","refresh_token":"new-refresh","expires_in":3600}`), nil
				}
				calls++
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatal(err)
				}
				if req.Method != http.MethodPost || string(body) != tc.wantBody || req.Header.Get("Content-Type") != tc.contentType {
					t.Errorf("replayed request = %s %q %q", req.Method, body, req.Header.Get("Content-Type"))
				}
				wantToken := "Bearer test-token"
				if calls > 1 {
					wantToken = "Bearer new-token"
				}
				if got := req.Header.Get("Authorization"); got != wantToken {
					t.Errorf("Authorization = %q, want %q", got, wantToken)
				}
				if calls == 1 {
					res := authTestResponse(req, 401, `{"error":"expired"}`)
					rejected = res.Body.(*authResponseBody)
					return res, nil
				}
				return authTestResponse(req, 200, `{}`), nil
			})
			res, err := inst.fetch(http.MethodPost, "/catalog/test/", tc.payload, tc.contentType)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if calls != 2 {
				t.Errorf("catalog calls = %d, want 2", calls)
			}
		})
	}
}

func TestAuthEndpointsDoNotRefreshOnUnauthorized(t *testing.T) {
	for _, endpoint := range []string{loginEndpoint, authEndpoint, tokenEndpoint} {
		t.Run(endpoint, func(t *testing.T) {
			calls := 0
			var body *authResponseBody
			inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				res := authTestResponse(req, 401, `{"error":"auth denied"}`)
				body = res.Body.(*authResponseBody)
				return res, nil
			})
			_, err := inst.fetch(http.MethodPost, endpoint, nil, "")
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Detail != "auth denied" || apiErr.Endpoint != endpoint || calls != 1 {
				t.Errorf("fetch() = %v after %d calls, want original auth error after one call", err, calls)
			}
			if !body.closed.Load() {
				t.Error("unauthorized auth body was not closed")
			}
		})
	}
}

func TestLoginAndAuthorizeCloseResponseBodies(t *testing.T) {
	for _, tc := range []struct {
		name, location, cookie string
		status                 int
		wantErr                bool
	}{
		{"login success", "", "sessionid=synthetic; Path=/", 200, false},
		{"login missing cookie", "", "", 200, true},
		{"login rejected", "", "", 401, true},
		{"authorize success", "https://example.invalid/?code=synthetic", "", 302, false},
		{"authorize missing code", "https://example.invalid/", "", 302, true},
		{"authorize malformed redirect", "://invalid", "", 302, true},
		{"authorize rejected", "", "", 401, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body *authResponseBody
			inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
				res := authTestResponse(req, tc.status, `{}`)
				res.Header.Set("Location", tc.location)
				res.Header.Set("Set-Cookie", tc.cookie)
				body = res.Body.(*authResponseBody)
				return res, nil
			})
			var err error
			if strings.HasPrefix(tc.name, "login") {
				_, err = inst.auth.login(inst)
			} else {
				_, err = inst.auth.authorize(inst, "synthetic")
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if !body.closed.Load() {
				t.Error("auth response body was not closed")
			}
		})
	}
}

func TestAuthMissingTokenState(t *testing.T) {
	for _, state := range []string{"nil auth", "nil token", "empty token"} {
		t.Run(state, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("missing token state panicked: %v", p)
				}
			}()
			inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
				return authTestResponse(req, 401, `{"detail":"synthetic login failure"}`), nil
			})
			switch state {
			case "nil auth":
				inst.auth = nil
			case "nil token":
				inst.auth.tokenPair = nil
				inst.auth.Invalidate()
			case "empty token":
				inst.auth.tokenPair = &tokenPair{}
			}
			if _, err := inst.fetch(http.MethodGet, "/catalog/tracks/42/", nil, ""); err == nil {
				t.Error("fetch() accepted missing authentication")
			}
		})
	}
}

func TestConcurrentChecksShareRefresh(t *testing.T) {
	var refreshCalls atomic.Int32
	inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
		refreshCalls.Add(1)
		// Keep callers overlapping at expiry without relying on external services.
		time.Sleep(time.Millisecond)
		return authTestResponse(req, 200, `{"access_token":"new-token","refresh_token":"new-refresh","expires_in":3600}`), nil
	})
	for round := 0; round < 10; round++ {
		inst.auth.Invalidate()
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if err := inst.auth.Check(inst); err != nil {
					t.Error(err)
				}
			}()
		}
		close(start)
		wg.Wait()
	}
	if got := refreshCalls.Load(); got != 10 {
		t.Errorf("refresh calls = %d, want one per expiry (10)", got)
	}
}

func TestConcurrentFetchAndTokenUpdates(t *testing.T) {
	inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, tokenEndpoint) {
			return authTestResponse(req, 200, `{"access_token":"new-token","refresh_token":"new-refresh","expires_in":3600}`), nil
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-token" && got != "Bearer new-token" {
			t.Errorf("incomplete token snapshot: %q", got)
		}
		return authTestResponse(req, 200, `{}`), nil
	})
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if worker == 0 {
					inst.auth.Invalidate()
				}
				res, err := inst.fetch(http.MethodGet, "/catalog/tracks/42/", nil, "")
				if err != nil {
					t.Error(err)
					return
				}
				res.Body.Close()
				runtime.Gosched()
			}
		}(worker)
	}
	wg.Wait()
	data, err := os.ReadFile(inst.auth.cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	var cached tokenPair
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatal(err)
	}
	if cached.AccessToken != "new-token" || cached.LoginID != inst.auth.loginId() {
		t.Errorf("cached token = %+v", cached)
	}
}

func TestAuthorizeCookieIsRequestLocal(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/auth/o/authorize/") {
			if got := req.Header.Get("Cookie"); got != "sessionid=synthetic" {
				t.Errorf("authorize cookie = %q", got)
			}
			close(entered)
			<-release
			res := authTestResponse(req, 302, "")
			res.Header.Set("Location", "https://example.invalid/?code=synthetic")
			return res, nil
		}
		if got := req.Header.Get("Cookie"); got != "" {
			t.Errorf("session cookie leaked into catalog request: %q", got)
		}
		return authTestResponse(req, 200, `{}`), nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := inst.auth.authorize(inst, "synthetic")
		done <- err
	}()
	<-entered
	res, err := inst.fetch(http.MethodGet, "/catalog/tracks/42/", nil, "")
	if err == nil {
		res.Body.Close()
	}
	close(release)
	if authErr := <-done; authErr != nil {
		t.Error(authErr)
	}
	if err != nil {
		t.Fatalf("catalog request: %v", err)
	}
}

func TestConcurrentUnauthorizedRequestsShareRecovery(t *testing.T) {
	const workers = 8
	var oldRequests, refreshCalls, replayCalls atomic.Int32
	allRejected := make(chan struct{})
	inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, tokenEndpoint) {
			refreshCalls.Add(1)
			return authTestResponse(req, 200, `{"access_token":"new-token","refresh_token":"new-refresh","expires_in":3600}`), nil
		}
		if req.Header.Get("Authorization") == "Bearer test-token" {
			if oldRequests.Add(1) == workers {
				close(allRejected)
			}
			<-allRejected
			return authTestResponse(req, 401, `{"detail":"old token"}`), nil
		}
		replayCalls.Add(1)
		return authTestResponse(req, 200, `{}`), nil
	})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := inst.fetch(http.MethodGet, "/catalog/tracks/42/", nil, "")
			if err != nil {
				t.Error(err)
				return
			}
			res.Body.Close()
		}()
	}
	wg.Wait()
	if refreshCalls.Load() != 1 || oldRequests.Load() != workers || replayCalls.Load() != workers {
		t.Errorf("old/refresh/replay calls = %d/%d/%d, want 8/1/8", oldRequests.Load(), refreshCalls.Load(), replayCalls.Load())
	}
}

func TestMissingOrRejectedTokenInitializesAndCaches(t *testing.T) {
	for _, initial := range []string{"missing", "refresh rejected"} {
		t.Run(initial, func(t *testing.T) {
			var refreshCalls, loginCalls, authorizeCalls, issueCalls atomic.Int32
			var bodiesMu sync.Mutex
			var bodies []*authResponseBody
			inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
				var res *http.Response
				switch {
				case strings.HasSuffix(req.URL.Path, tokenEndpoint):
					if err := req.ParseForm(); err != nil {
						t.Error(err)
						return nil, err
					}
					if req.Form.Get("grant_type") == "refresh_token" {
						refreshCalls.Add(1)
						res = authTestResponse(req, 401, `{"error":"refresh rejected"}`)
					} else {
						issueCalls.Add(1)
						if req.Form.Get("grant_type") != "authorization_code" || req.Form.Get("code") != "synthetic-code" {
							t.Errorf("unexpected token form: %v", req.Form)
						}
						res = authTestResponse(req, 200, `{"access_token":"issued-token","refresh_token":"issued-refresh","expires_in":3600}`)
					}
				case strings.HasSuffix(req.URL.Path, loginEndpoint):
					loginCalls.Add(1)
					res = authTestResponse(req, 200, `{}`)
					res.Header.Set("Set-Cookie", "sessionid=synthetic-session; Path=/")
				case strings.HasSuffix(req.URL.Path, "/auth/o/authorize/"):
					authorizeCalls.Add(1)
					if req.Header.Get("Cookie") != "sessionid=synthetic-session" {
						t.Errorf("authorize cookie = %q", req.Header.Get("Cookie"))
					}
					res = authTestResponse(req, 302, "")
					res.Header.Set("Location", "https://example.invalid/?code=synthetic-code")
				default:
					if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "Bearer issued-token" {
						t.Errorf("unexpected catalog headers: %v", req.Header)
					}
					res = authTestResponse(req, 200, `{}`)
				}
				bodiesMu.Lock()
				bodies = append(bodies, res.Body.(*authResponseBody))
				bodiesMu.Unlock()
				return res, nil
			})
			if initial == "missing" {
				inst.auth.tokenPair = nil
			} else {
				inst.auth.Invalidate()
			}
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					res, err := inst.fetch(http.MethodGet, "/catalog/tracks/42/", nil, "")
					if err != nil {
						t.Error(err)
						return
					}
					res.Body.Close()
				}()
			}
			wg.Wait()
			wantRefresh := int32(0)
			if initial == "refresh rejected" {
				wantRefresh = 1
			}
			if refreshCalls.Load() != wantRefresh || loginCalls.Load() != 1 || authorizeCalls.Load() != 1 || issueCalls.Load() != 1 {
				t.Errorf("refresh/login/authorize/issue calls = %d/%d/%d/%d", refreshCalls.Load(), loginCalls.Load(), authorizeCalls.Load(), issueCalls.Load())
			}
			for _, body := range bodies {
				if !body.closed.Load() {
					t.Error("fallback login or catalog left a response body open")
				}
			}
			loaded := NewAuth("", "", inst.auth.cacheFile)
			if err := loaded.LoadCache(); err != nil {
				t.Fatal(err)
			}
			if token := loaded.snapshot(); token.AccessToken != "issued-token" || token.RefreshToken != "issued-refresh" || token.LoginID != inst.auth.loginId() || token.IssuedAt == 0 {
				t.Errorf("cached issued token = %+v", token)
			}
		})
	}
}

func TestConcurrentCacheAndTokenOperations(t *testing.T) {
	inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
		return authTestResponse(req, 200, `{"access_token":"test-token","refresh_token":"test-refresh","expires_in":3600}`), nil
	})
	if err := inst.auth.WriteCache(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, operation := range []func() error{
		inst.auth.LoadCache,
		inst.auth.WriteCache,
		func() error { inst.auth.Invalidate(); return inst.auth.Check(inst) },
		func() error {
			res, err := inst.fetch(http.MethodGet, "/catalog/tracks/42/", nil, "")
			if err == nil {
				res.Body.Close()
			}
			return err
		},
	} {
		wg.Add(1)
		go func(operation func() error) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				if err := operation(); err != nil {
					t.Error(err)
					return
				}
			}
		}(operation)
	}
	wg.Wait()
}

func TestMalformedTokenResponseDoesNotReplaceExistingToken(t *testing.T) {
	for _, payload := range []string{"null", "{}", "not json"} {
		for _, operation := range []string{"refresh", "issue"} {
			t.Run(operation+"/"+payload, func(t *testing.T) {
				var body *authResponseBody
				inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
					res := authTestResponse(req, 200, payload)
					body = res.Body.(*authResponseBody)
					return res, nil
				})
				var err error
				if operation == "refresh" {
					_, err = inst.auth.refresh(inst)
				} else {
					err = inst.auth.issue(inst, "synthetic")
				}
				if err == nil {
					t.Error("malformed token response was accepted")
				}
				if token := inst.auth.snapshot(); token.AccessToken != "test-token" {
					t.Errorf("malformed response replaced the token: %+v", token)
				}
				if !body.closed.Load() {
					t.Error("malformed token response body remains open")
				}
			})
		}
	}
}

package beatport

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestRetryDelayStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstAttempt := make(chan struct{})
	attempts := 0
	inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			close(firstAttempt)
			return nil, context.DeadlineExceeded
		}
		return authTestResponse(req, 200, `{}`), nil
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid/", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := inst.doRequest(req)
		done <- err
	}()
	<-firstAttempt
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("doRequest() = %v, want cancellation", err)
		}
	case <-time.After(150 * time.Millisecond):
		t.Error("retry delay did not stop promptly on cancellation")
		<-done
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

func TestAuthCancellationDoesNotFallBackToLogin(t *testing.T) {
	for _, wantErr := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(wantErr.Error(), func(t *testing.T) {
			calls := 0
			inst := authTestClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if !strings.HasSuffix(req.URL.Path, tokenEndpoint) {
					t.Error("canceled refresh fell back to password login")
				}
				return nil, wantErr
			})
			inst.auth.Invalidate()
			err := inst.auth.Check(inst)
			if !errors.Is(err, wantErr) || calls != 1 {
				t.Errorf("Check() = %v after %d calls, want %v after one call", err, calls, wantErr)
			}
		})
	}
}

func TestWithContextCancelsStalledRequests(t *testing.T) {
	for _, endpoint := range []string{"/catalog/tracks/42/", loginEndpoint, authEndpoint, tokenEndpoint} {
		t.Run(endpoint, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			var calls atomic.Int32
			base := authTestClient(t, func(req *http.Request) (*http.Response, error) {
				if calls.Add(1) != 1 {
					t.Error("cancellation triggered another HTTP request")
					return nil, context.Canceled
				}
				close(entered)
				<-req.Context().Done()
				return nil, req.Context().Err()
			})
			bound := base.WithContext(ctx)
			done := make(chan error, 1)
			go func() {
				_, err := bound.fetch(http.MethodGet, endpoint, nil, "")
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("request did not start")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("fetch() = %v, want cancellation", err)
				}
			case <-time.After(time.Second):
				t.Fatal("stalled request did not stop on cancellation")
			}
			if base.requestContext().Err() != nil {
				t.Error("WithContext mutated the original client's context")
			}
			if calls.Load() != 1 {
				t.Errorf("HTTP calls = %d, want 1", calls.Load())
			}
		})
	}
}

func TestWithContextCancellationStopsRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	var calls atomic.Int32
	base := authTestClient(t, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if !strings.HasSuffix(req.URL.Path, tokenEndpoint) {
			t.Error("canceled refresh started login or catalog request")
			return nil, context.Canceled
		}
		close(entered)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	base.auth.Invalidate()
	bound := base.WithContext(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := bound.fetch(http.MethodGet, "/catalog/tracks/42/", nil, "")
		done <- err
	}()
	<-entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("fetch() = %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh did not stop on cancellation")
	}
	if calls.Load() != 1 {
		t.Errorf("HTTP calls = %d, want only the refresh", calls.Load())
	}
}

func TestWithContextKeepsOriginalClientUsable(t *testing.T) {
	base := New("", nil)
	base.auth = authTestClient(t, nil).auth
	base.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return authTestResponse(req, 200, `{}`), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	bound := base.WithContext(ctx)
	cancel()
	if bound == base || bound.auth != base.auth || bound.client != base.client {
		t.Fatal("WithContext did not return a shallow copy")
	}
	if _, err := bound.fetch(http.MethodGet, "/catalog/tracks/42/", nil, ""); !errors.Is(err, context.Canceled) {
		t.Errorf("bound fetch() = %v, want cancellation", err)
	}
	res, err := base.fetch(http.MethodGet, "/catalog/tracks/42/", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
}

type observedAuthContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (c *observedAuthContext) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.checked) })
	return err
}

func TestCancellationStopsWaitingForSharedRefresh(t *testing.T) {
	refreshStarted, releaseRefresh := make(chan struct{}), make(chan struct{})
	base := authTestClient(t, func(req *http.Request) (*http.Response, error) {
		close(refreshStarted)
		<-releaseRefresh
		return authTestResponse(req, 200, `{"access_token":"new-token","refresh_token":"new-refresh","expires_in":3600}`), nil
	})
	base.auth.Invalidate()
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- base.auth.Check(base) }()
	<-refreshStarted
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := &observedAuthContext{Context: ctx, checked: make(chan struct{})}
	bound := base.WithContext(observed)
	waiterDone := make(chan error, 1)
	go func() { waiterDone <- bound.auth.Check(bound) }()
	<-observed.checked
	cancel()
	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Check() = %v, want cancellation", err)
		}
	case <-time.After(150 * time.Millisecond):
		t.Error("canceled client remained blocked behind another client's refresh")
		close(releaseRefresh)
		<-refreshDone
		<-waiterDone
		return
	}
	close(releaseRefresh)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
}

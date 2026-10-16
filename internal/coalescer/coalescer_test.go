package coalescer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHub_CoalescesDuplicateRequests(t *testing.T) {
	hub := New[string]()
	var loaderCalls atomic.Int32

	loader := func(ctx context.Context) (string, error) {
		loaderCalls.Add(1)
		time.Sleep(50 * time.Millisecond) // simulate slow query
		return "hello-cache", nil
	}

	const callers = 50
	var wg sync.WaitGroup
	results := make([]string, callers)
	errs := make([]error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			val, err := hub.Do(context.Background(), "user:1", loader)
			results[idx] = val
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	if calls := loaderCalls.Load(); calls != 1 {
		t.Fatalf("expected exactly 1 loader call, got %d", calls)
	}

	for i, res := range results {
		if errs[i] != nil {
			t.Fatalf("caller %d received error: %v", i, errs[i])
		}
		if res != "hello-cache" {
			t.Fatalf("caller %d expected 'hello-cache', got '%s'", i, res)
		}
	}

	if hub.InFlightCount() != 0 {
		t.Fatalf("expected 0 in-flight calls after completion, got %d", hub.InFlightCount())
	}
}

func TestHub_DifferentKeysExecuteIndependently(t *testing.T) {
	hub := New[int]()
	var loaderCalls atomic.Int32

	loader := func(ctx context.Context) (int, error) {
		loaderCalls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return 42, nil
	}

	var wg sync.WaitGroup
	keys := []string{"k1", "k2", "k3", "k4", "k5"}

	for _, k := range keys {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			val, err := hub.Do(context.Background(), key, loader)
			if err != nil || val != 42 {
				t.Errorf("key %s failed: val=%d, err=%v", key, val, err)
			}
		}(k)
	}

	wg.Wait()

	if calls := loaderCalls.Load(); calls != int32(len(keys)) {
		t.Fatalf("expected %d distinct loader calls, got %d", len(keys), calls)
	}
}

func TestHub_FollowerCancellationDoesNotAffectLeader(t *testing.T) {
	hub := New[string]()
	started := make(chan struct{})

	loader := func(ctx context.Context) (string, error) {
		close(started)
		time.Sleep(100 * time.Millisecond)
		return "complete-result", nil
	}

	// 1. Leader starts
	leaderDone := make(chan struct{})
	var leaderVal string
	var leaderErr error
	go func() {
		defer close(leaderDone)
		leaderVal, leaderErr = hub.Do(context.Background(), "key1", loader)
	}()

	<-started

	// 2. Follower joins with cancellable context
	followerCtx, followerCancel := context.WithCancel(context.Background())
	followerDone := make(chan struct{})
	var followerVal string
	var followerErr error
	go func() {
		defer close(followerDone)
		followerVal, followerErr = hub.Do(followerCtx, "key1", loader)
	}()

	// 3. Cancel follower immediately
	time.Sleep(10 * time.Millisecond)
	followerCancel()

	<-followerDone
	<-leaderDone

	// Follower must get context.Canceled and zero value
	if !errors.Is(followerErr, context.Canceled) {
		t.Fatalf("follower should receive context.Canceled, got: %v", followerErr)
	}
	if followerVal != "" {
		t.Fatalf("expected empty value for canceled follower, got: %s", followerVal)
	}

	// Leader must successfully complete
	if leaderErr != nil {
		t.Fatalf("leader failed unexpectedly: %v", leaderErr)
	}
	if leaderVal != "complete-result" {
		t.Fatalf("leader expected 'complete-result', got: '%s'", leaderVal)
	}
}

func TestHub_LeaderCancellationDoesNotBreakFollower(t *testing.T) {
	hub := New[string]()
	started := make(chan struct{})

	loader := func(ctx context.Context) (string, error) {
		close(started)
		time.Sleep(100 * time.Millisecond)
		return "rescued-by-follower", nil
	}

	// 1. Leader starts with cancellable context
	leaderCtx, leaderCancel := context.WithCancel(context.Background())
	leaderDone := make(chan struct{})
	var leaderErr error
	go func() {
		defer close(leaderDone)
		_, leaderErr = hub.Do(leaderCtx, "key2", loader)
	}()

	<-started

	// 2. Follower joins with normal background context
	followerDone := make(chan struct{})
	var followerVal string
	var followerErr error
	go func() {
		defer close(followerDone)
		followerVal, followerErr = hub.Do(context.Background(), "key2", loader)
	}()

	// 3. Cancel leader
	time.Sleep(10 * time.Millisecond)
	leaderCancel()

	<-leaderDone
	<-followerDone

	// Leader was canceled
	if !errors.Is(leaderErr, context.Canceled) {
		t.Fatalf("leader expected context.Canceled, got: %v", leaderErr)
	}

	// Follower must receive the completed value
	if followerErr != nil {
		t.Fatalf("follower received unexpected error: %v", followerErr)
	}
	if followerVal != "rescued-by-follower" {
		t.Fatalf("follower expected 'rescued-by-follower', got '%s'", followerVal)
	}
}

func TestHub_PanicSafety(t *testing.T) {
	hub := New[string]()

	loader := func(ctx context.Context) (string, error) {
		panic("database connection exploded!")
	}

	const callers = 5
	var wg sync.WaitGroup
	errs := make([]error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := hub.Do(context.Background(), "panic-key", loader)
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if !errors.Is(err, ErrLoaderPanicked) {
			t.Fatalf("caller %d expected ErrLoaderPanicked, got: %v", i, err)
		}
	}

	if hub.InFlightCount() != 0 {
		t.Fatalf("expected map cleanup after panic, in-flight: %d", hub.InFlightCount())
	}
}

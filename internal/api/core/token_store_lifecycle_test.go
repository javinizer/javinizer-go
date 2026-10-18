package core

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// A borrowed implementation still satisfies the original handler interface.
// Shutdown must not assume ownership of an external implementation's resources.
type borrowedTokenStore struct{ closed bool }

func (*borrowedTokenStore) Create(string, string) (string, time.Time, error) {
	return "", time.Time{}, nil
}
func (*borrowedTokenStore) Validate(string, string, string) bool { return false }
func (*borrowedTokenStore) CleanupExpired()                      {}
func (s *borrowedTokenStore) Close()                             { s.closed = true }

func TestCloseTokenStoreNilAndBorrowed(t *testing.T) {
	CloseTokenStore(nil)
	var nilStore *tokenStore
	CloseTokenStore(nilStore)
	borrowed := &borrowedTokenStore{}
	CloseTokenStore(borrowed)
	rt := NewAPIRuntime(&APIDeps{TokenStore: borrowed})
	rt.Shutdown()
	require.False(t, borrowed.closed)
	(&APIRuntime{}).Shutdown()
}

func TestTokenStoreCleanupAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := NewTokenStore().(*tokenStore)
		t.Cleanup(func() { CloseTokenStore(store) })
		token, _, err := store.Create("global", "hash")
		require.NoError(t, err)
		synctest.Wait() // worker is waiting on its first ticker
		time.Sleep(6 * time.Minute)
		require.False(t, store.Validate(token, "global", "hash"))
		store.mu.RLock()
		_, exists := store.tokens[token]
		store.mu.RUnlock()
		require.True(t, exists, "expiration must reject before periodic reclamation")
		time.Sleep(4 * time.Minute)
		synctest.Wait()
		store.mu.RLock()
		_, exists = store.tokens[token]
		store.mu.RUnlock()
		require.False(t, exists, "10-minute cleanup must reclaim expired tokens")
		CloseTokenStore(store)
		select {
		case <-store.done:
		default:
			t.Fatal("close returned before worker exited")
		}
		token, _, err = store.Create("global", "hash")
		require.NoError(t, err)
		require.True(t, store.Validate(token, "global", "hash"))
		require.False(t, store.Validate(token, "other", "hash"))
		require.False(t, store.Validate(token, "global", "changed"))
		time.Sleep(11 * time.Minute)
		require.False(t, store.Validate(token, "global", "hash"))
		store.mu.RLock()
		_, exists = store.tokens[token]
		store.mu.RUnlock()
		require.True(t, exists, "closed worker must not perform later cleanup")
		store.CleanupExpired()
		store.mu.RLock()
		_, exists = store.tokens[token]
		store.mu.RUnlock()
		require.False(t, exists, "manual cleanup remains usable")
	})
}

func TestTokenStoreConcurrentCloseAndOperations(t *testing.T) {
	store := NewTokenStore().(*tokenStore)
	t.Cleanup(func() { CloseTokenStore(store) })
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 32 {
		wg.Go(func() {
			<-start
			for range 20 {
				token, _, err := store.Create("global", "hash")
				if err != nil {
					t.Error(err)
					return
				}
				if !store.Validate(token, "global", "hash") {
					t.Error("valid token rejected")
				}
				store.CleanupExpired()
				CloseTokenStore(store)
				select {
				case <-store.done:
				default:
					t.Error("close returned before worker exited")
				}
			}
		})
	}
	close(start)
	wg.Wait()
}

func TestAPIRuntimeShutdownJoinsTokenStore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := NewTokenStore().(*tokenStore)
		t.Cleanup(func() { CloseTokenStore(store) })
		rt := NewAPIRuntime(&APIDeps{TokenStore: store})
		state := rt.EnsureRuntime()
		state.ResetWebSocketHub()
		state.ResetWebSocketHub()
		state.Shutdown()
		select {
		case <-store.done:
			t.Fatal("WebSocket hub reset/shutdown must not stop token cleanup")
		default:
		}
		var wg sync.WaitGroup
		for range 16 {
			wg.Go(rt.Shutdown)
		}
		wg.Wait()
		select {
		case <-store.done:
		default:
			t.Fatal("runtime shutdown did not join token cleanup")
		}
		require.Error(t, rt.ServerCtx().Err())
	})
}

func TestTokenStoreSharedRuntimeOwnership(t *testing.T) {
	store := NewTokenStore().(*tokenStore)
	t.Cleanup(func() { CloseTokenStore(store) })
	first := NewAPIRuntime(&APIDeps{TokenStore: store})
	second := NewAPIRuntime(&APIDeps{TokenStore: store})
	first.Shutdown()
	select {
	case <-store.done:
	default:
		t.Fatal("first owner must join cleanup")
	}
	token, _, err := second.Deps().TokenStore.Create("global", "hash")
	require.NoError(t, err)
	require.True(t, second.Deps().TokenStore.Validate(token, "global", "hash"))
	second.Shutdown()
	first.Shutdown()
}

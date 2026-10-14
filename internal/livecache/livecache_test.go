package livecache

import (
	"sync"
	"testing"
	"time"
)

// TestPutGetFreshStale 验证缓存读写的过期语义:
//   - Put 后可 Get 到;
//   - maxAge 内 GetFresh 命中;
//   - 超过 maxAge 视为过期(返回 false,让 handler 回退实时采集)。
func TestPutGetFreshStale(t *testing.T) {
	s := New()
	s.Put("metrics", "srv-1", "snapshot-A")

	if v, ok := s.Get("metrics", "srv-1"); !ok || v != "snapshot-A" {
		t.Fatalf("Get 应返回刚写入的值, got %v ok=%v", v, ok)
	}
	// maxAge 充裕 → 命中。
	if v, ok := s.GetFresh("metrics", "srv-1", time.Minute); !ok || v != "snapshot-A" {
		t.Fatalf("GetFresh 未过期时应命中, got %v ok=%v", v, ok)
	}
	// maxAge=0 → 不判过期(语义)。
	if _, ok := s.GetFresh("metrics", "srv-1", 0); !ok {
		t.Fatal("GetFresh maxAge<=0 应不判过期并命中")
	}
	// 未写入的 key → 未命中。
	if _, ok := s.GetFresh("metrics", "srv-2", time.Minute); ok {
		t.Fatal("未写入的 key 不应命中")
	}
	// 人为把 entry 时间推到过去 → 过期。
	s.mu.Lock()
	e := s.cache[s.key("metrics", "srv-1")]
	e.at = time.Now().Add(-2 * time.Minute)
	s.cache[s.key("metrics", "srv-1")] = e
	s.mu.Unlock()
	if _, ok := s.GetFresh("metrics", "srv-1", 30*time.Second); ok {
		t.Fatal("超过 maxAge 应判定过期(未命中)")
	}
}

// TestIntervalCachedNotReadPerRequest 是「轻量化、不在请求热路径查库」的回归测试:
// intervalFn(内部会读 system_settings)只允许在 tick 边界被调一次,
// 此后无论多少次 GetFresh / StaleAfter(即多少次 HTTP 请求)都不得再触发。
func TestIntervalCachedNotReadPerRequest(t *testing.T) {
	s := New()
	calls := 0
	s.SetIntervalFn(func() time.Duration {
		calls++
		return 60 * time.Second
	})

	// 模拟 Start 时的首次刷新。
	iv := s.refreshInterval()
	if iv != 60*time.Second {
		t.Fatalf("refreshInterval 应返回 60s, got %v", iv)
	}
	if calls != 1 {
		t.Fatalf("refreshInterval 应只调用 intervalFn 一次, got %d", calls)
	}
	// 校验缓存值已被记录。
	if got := s.tickInterval(); got != 60*time.Second {
		t.Fatalf("tickInterval 应返回缓存的 60s, got %v", got)
	}
	if got := s.StaleAfter(); got != 240*time.Second {
		t.Fatalf("StaleAfter 应为间隔 4 倍 =240s, got %v", got)
	}

	// 模拟 1000 次 HTTP 请求读缓存:intervalFn 绝不能再被调用。
	s.Put("metrics", "srv-1", "v")
	for i := 0; i < 1000; i++ {
		_, _ = s.GetFresh("metrics", "srv-1", s.StaleAfter())
	}
	if calls != 1 {
		t.Fatalf("请求热路径不得再查库:intervalFn 被调 %d 次(期望 1)", calls)
	}
}

// TestStaleAfterDefault 未配置 intervalFn 时回落默认 30s,StaleAfter 至少 2 分钟。
func TestStaleAfterDefault(t *testing.T) {
	s := New() // 未 SetIntervalFn
	if got := s.tickInterval(); got != defaultInterval {
		t.Fatalf("默认间隔应为 %v, got %v", defaultInterval, got)
	}
	if got := s.StaleAfter(); got != defaultInterval*4 {
		t.Fatalf("默认 StaleAfter 应为 %v, got %v", defaultInterval*4, got)
	}
}

// TestConcurrentPutGet 并发读写安全(配合 -race 运行)。
func TestConcurrentPutGet(t *testing.T) {
	s := New()
	s.SetIntervalFn(func() time.Duration { return 30 * time.Second })
	_ = s.refreshInterval()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Put("metrics", "srv", i)
			_, _ = s.Get("metrics", "srv")
			_, _ = s.GetFresh("metrics", "srv", s.StaleAfter())
		}(i)
	}
	wg.Wait()
	if _, ok := s.Get("metrics", "srv"); !ok {
		t.Fatal("并发写入后应能读到值")
	}
}

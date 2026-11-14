package retry_test

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/retry"
)

// window is the contract's sliding window length.
const window = 10 * time.Second

// fakeClock is the injected clock; tests advance it instead of sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func mustBudget(t *testing.T, ratio float64, minRetries int, clk func() time.Time) *retry.Budget {
	t.Helper()
	b, err := retry.NewBudget(ratio, minRetries, clk)
	if err != nil {
		t.Fatalf("NewBudget(%v, %d): unexpected error %v", ratio, minRetries, err)
	}
	if b == nil {
		t.Fatalf("NewBudget(%v, %d) returned a nil budget with a nil error", ratio, minRetries)
	}
	return b
}

func record(b *retry.Budget, n int) {
	for i := 0; i < n; i++ {
		b.RecordRequest()
	}
}

func wantStats(t *testing.T, b *retry.Budget, requests, retries int) {
	t.Helper()
	got := b.Stats()
	want := retry.Stats{Requests: requests, Retries: retries}
	if got != want {
		t.Fatalf("Stats() = %+v, want %+v", got, want)
	}
}

func wantAllow(t *testing.T, b *retry.Budget, want bool) {
	t.Helper()
	if got := b.Allow(); got != want {
		t.Fatalf("Allow() = %v, want %v (stats %+v)", got, want, b.Stats())
	}
}

// R1: constructor errors.
func TestNewBudgetRejectsInvalidArguments_R1(t *testing.T) {
	clk := newClock()
	cases := []struct {
		name  string
		ratio float64
		min   int
		clock func() time.Time
		want  error
	}{
		{"ratio zero", 0, 0, clk.Now, retry.ErrInvalidRatio},
		{"ratio negative", -0.5, 0, clk.Now, retry.ErrInvalidRatio},
		{"ratio just above one", math.Nextafter(1, 2), 0, clk.Now, retry.ErrInvalidRatio},
		{"ratio two", 2, 0, clk.Now, retry.ErrInvalidRatio},
		{"min negative", 0.5, -1, clk.Now, retry.ErrInvalidMin},
		{"nil clock", 0.5, 0, nil, retry.ErrNilClock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := retry.NewBudget(tc.ratio, tc.min, tc.clock)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewBudget(%v, %d, clock) error = %v, want %v", tc.ratio, tc.min, err, tc.want)
			}
		})
	}
}

// R1: boundary values that are valid.
func TestNewBudgetAcceptsBoundaries_R1(t *testing.T) {
	clk := newClock()
	for _, ratio := range []float64{1, math.SmallestNonzeroFloat64, 0.5} {
		b, err := retry.NewBudget(ratio, 0, clk.Now)
		if err != nil || b == nil {
			t.Fatalf("NewBudget(%v, 0, clock) = (%v, %v), want a budget and nil error", ratio, b, err)
		}
	}
}

// R2 and R7: each RecordRequest counts one first attempt.
func TestRecordRequestCounts_R2_R7(t *testing.T) {
	clk := newClock()
	b := mustBudget(t, 0.5, 0, clk.Now)
	wantStats(t, b, 0, 0)
	b.RecordRequest()
	wantStats(t, b, 1, 0)
	record(b, 2)
	wantStats(t, b, 3, 0)
}

// R3 and R4: the number of true results equals max(minRetries, ceil(ratio*requests)),
// and they all come before the first false.
func TestAllowLimit_R3_R4(t *testing.T) {
	cases := []struct {
		ratio    float64
		min      int
		requests int
		want     int
	}{
		{0.2, 1, 10, 2},
		{0.2, 1, 0, 1},
		{0.5, 0, 0, 0},
		{0.2, 0, 7, 2},  // ceil(1.4)
		{0.5, 0, 3, 2},  // ceil(1.5)
		{0.3, 0, 7, 3},  // ceil(2.1)
		{0.2, 0, 3, 1},  // ceil(0.6)
		{0.1, 0, 1, 1},  // ceil(0.1)
		{1, 0, 5, 5},    // ratio 1
		{0.5, 3, 2, 3},  // minRetries dominates
		{0.2, 3, 20, 4}, // limit dominates
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("ratio=%v/min=%d/requests=%d", tc.ratio, tc.min, tc.requests), func(t *testing.T) {
			clk := newClock()
			b := mustBudget(t, tc.ratio, tc.min, clk.Now)
			record(b, tc.requests)
			trues := 0
			sawFalse := false
			for i := 0; i < tc.want+10; i++ {
				if b.Allow() {
					if sawFalse {
						t.Fatalf("Allow() returned true after a false at call %d", i+1)
					}
					trues++
				} else {
					sawFalse = true
				}
			}
			if trues != tc.want {
				t.Fatalf("Allow() returned true %d times, want %d", trues, tc.want)
			}
			wantStats(t, b, tc.requests, tc.want)
		})
	}
}

// R4: a false result counts nothing, so raising the limit later allows again.
func TestFalseCountsNothing_R4(t *testing.T) {
	clk := newClock()
	b := mustBudget(t, 0.5, 0, clk.Now)
	record(b, 2) // limit 1
	wantAllow(t, b, true)
	for i := 0; i < 5; i++ {
		wantAllow(t, b, false)
	}
	wantStats(t, b, 2, 1)
	record(b, 2) // limit 2
	wantAllow(t, b, true)
	wantAllow(t, b, false)
	wantStats(t, b, 4, 2)
}

// R5 and R7: events exactly 10 seconds old are counted; older ones are not.
func TestWindowBoundary_R5_R7(t *testing.T) {
	clk := newClock()
	b := mustBudget(t, 0.2, 0, clk.Now)
	record(b, 10) // limit 2
	wantAllow(t, b, true)
	wantAllow(t, b, true)
	wantAllow(t, b, false)
	clk.Advance(window) // exactly 10s old: still counted
	wantStats(t, b, 10, 2)
	wantAllow(t, b, false)
	clk.Advance(time.Nanosecond) // more than 10s old: not counted
	wantStats(t, b, 0, 0)
	wantAllow(t, b, false) // max(0, ceil(0)) = 0
	record(b, 10)          // limit ceil(2.0) = 2
	wantAllow(t, b, true)
	wantAllow(t, b, true)
	wantAllow(t, b, false)
	wantStats(t, b, 10, 2)
}

// R5 and R7: the window slides per event rather than resetting in buckets.
func TestWindowSlides_R5_R7(t *testing.T) {
	clk := newClock()
	b := mustBudget(t, 1, 0, clk.Now)
	b.RecordRequest() // t0
	wantAllow(t, b, true)
	wantStats(t, b, 1, 1)
	clk.Advance(5 * time.Second) // t0+5s
	b.RecordRequest()
	wantAllow(t, b, true)
	wantStats(t, b, 2, 2)
	clk.Advance(5 * time.Second) // t0+10s: everything still counted
	wantStats(t, b, 2, 2)
	wantAllow(t, b, false)
	clk.Advance(time.Nanosecond) // t0 events dropped, t0+5s kept
	wantStats(t, b, 1, 1)
	wantAllow(t, b, false)
	b.RecordRequest()
	wantAllow(t, b, true)
	wantStats(t, b, 2, 2)
	clk.Advance(5 * time.Second) // t0+15s+1ns: t0+5s events dropped
	wantStats(t, b, 1, 1)
	clk.Advance(window) // everything dropped
	wantStats(t, b, 0, 0)
}

// R5: time comes only from the injected clock (a frozen clock far in the past
// or future keeps every event in the window).
func TestTimeOnlyFromClock_R5(t *testing.T) {
	for _, frozen := range []time.Time{
		time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		at := frozen
		b := mustBudget(t, 0.5, 0, func() time.Time { return at })
		record(b, 4)
		wantAllow(t, b, true)
		wantAllow(t, b, true)
		wantAllow(t, b, false)
		wantStats(t, b, 4, 2)
	}
}

// R6: concurrent Allow calls never exceed the limit; with a fixed clock
// exactly the limit succeeds.
func TestConcurrentAllowRespectsLimit_R6(t *testing.T) {
	clk := newClock()
	b := mustBudget(t, 0.2, 1, clk.Now)
	record(b, 100) // limit max(1, 20) = 20
	var trues atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2; i++ {
				if b.Allow() {
					trues.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if got := trues.Load(); got != 20 {
		t.Fatalf("concurrent Allow returned true %d times, want 20", got)
	}
	wantStats(t, b, 100, 20)
}

// R6 and R2: concurrent RecordRequest and Allow keep counts consistent and
// within max(minRetries, ceil(ratio*requests)).
func TestConcurrentMixedUse_R6(t *testing.T) {
	clk := newClock()
	b := mustBudget(t, 0.2, 1, clk.Now)
	var trues atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				b.RecordRequest()
				if b.Allow() {
					trues.Add(1)
				}
				_ = b.Stats()
			}
		}()
	}
	wg.Wait()
	limit := int(math.Ceil(0.2 * float64(80)))
	if limit < 1 {
		limit = 1
	}
	got := int(trues.Load())
	if got > limit {
		t.Fatalf("concurrent Allow returned true %d times, limit %d", got, limit)
	}
	wantStats(t, b, 80, got)
}

// budgetModel is the in-test model: timestamps of requests and retries.
type budgetModel struct {
	ratio    float64
	min      int
	requests []time.Time
	retries  []time.Time
}

func inWindow(now time.Time, events []time.Time) int {
	n := 0
	for _, e := range events {
		if now.Sub(e) <= window {
			n++
		}
	}
	return n
}

func (m *budgetModel) allow(now time.Time) bool {
	limit := int(math.Ceil(m.ratio * float64(inWindow(now, m.requests))))
	if m.min > limit {
		limit = m.min
	}
	if inWindow(now, m.retries) < limit {
		m.retries = append(m.retries, now)
		return true
	}
	return false
}

// R2-R5, R7 property: for arbitrary sequences of requests, retries and clock
// advances, Allow and Stats agree with the model.
func TestModelBasedSequences_R2_R3_R4_R5_R7(t *testing.T) {
	ratios := []float64{0.05, 0.2, 0.35, 0.5, 0.7, 1}
	mins := []int{0, 1, 3}
	steps := []time.Duration{0, time.Second, 4 * time.Second, window - time.Nanosecond, window, window + time.Nanosecond}
	for seed := int64(1); seed <= 200; seed++ {
		rng := rand.New(rand.NewSource(seed))
		m := &budgetModel{ratio: ratios[rng.Intn(len(ratios))], min: mins[rng.Intn(len(mins))]}
		clk := newClock()
		b := mustBudget(t, m.ratio, m.min, clk.Now)
		var trace []string
		for op := 0; op < 150; op++ {
			now := clk.Now()
			switch rng.Intn(5) {
			case 0, 1:
				trace = append(trace, "RecordRequest")
				b.RecordRequest()
				m.requests = append(m.requests, now)
			case 2, 3:
				trace = append(trace, "Allow")
				got := b.Allow()
				want := m.allow(now)
				if got != want {
					t.Fatalf("seed %d ratio %v min %d: Allow() = %v, model %v; trace %v", seed, m.ratio, m.min, got, want, trace)
				}
			case 4:
				d := steps[rng.Intn(len(steps))]
				trace = append(trace, fmt.Sprintf("Advance(%v)", d))
				clk.Advance(d)
			}
			now = clk.Now()
			want := retry.Stats{Requests: inWindow(now, m.requests), Retries: inWindow(now, m.retries)}
			if got := b.Stats(); got != want {
				t.Fatalf("seed %d ratio %v min %d: Stats() = %+v, model %+v; trace %v", seed, m.ratio, m.min, got, want, trace)
			}
		}
	}
}

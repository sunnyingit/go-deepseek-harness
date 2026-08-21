package llmretry

import (
	"context"
	"errors"
	"testing"
	"time"
)

// classifiedError is a minimal classifiable for tests — llm/deepseek.Error
// is the real implementation, this avoids an import cycle-free dependency
// just for test fixtures.
type classifiedError struct {
	code       string
	retryAfter time.Duration
}

func (e classifiedError) Error() string                     { return "classified: " + e.code }
func (e classifiedError) RetryCode() string                 { return e.code }
func (e classifiedError) ProviderRetryDelay() time.Duration { return e.retryAfter }

// recordingSleeper captures every delay Hook asks it to wait and always
// returns true (as if the wait completed) — lets tests assert exact backoff
// values without a real test taking seconds.
func recordingSleeper() (*[]time.Duration, func(ctx context.Context, d time.Duration) bool) {
	var delays []time.Duration
	return &delays, func(_ context.Context, d time.Duration) bool {
		delays = append(delays, d)
		return true
	}
}

func fixedRandom(v float64) func() float64 { return func() float64 { return v } }

func TestHookNormalModeIgnoresUnlistedCode(t *testing.T) {
	delays, sleep := recordingSleeper()
	hook := Hook(NormalPolicy(), Internals{Sleep: sleep, Random: fixedRandom(0.5)})

	retried := hook(context.Background(), 1, 1, classifiedError{code: "INVALID_REQUEST"})

	if retried {
		t.Fatal("hook retried a non-retryable code under normal mode")
	}
	if len(*delays) != 0 {
		t.Fatalf("expected no sleep, got %v", *delays)
	}
}

func TestHookNormalModeRetriesListedCodeUpToMax(t *testing.T) {
	_, sleep := recordingSleeper()
	policy := NormalPolicy()
	policy.MaxRetries = 2
	hook := Hook(policy, Internals{Sleep: sleep, Random: fixedRandom(0.5)})

	failure := classifiedError{code: "SERVER"}
	if !hook(context.Background(), 1, 1, failure) {
		t.Fatal("1st retry: expected true")
	}
	if !hook(context.Background(), 1, 1, failure) {
		t.Fatal("2nd retry: expected true")
	}
	if hook(context.Background(), 1, 1, failure) {
		t.Fatal("3rd attempt exceeds MaxRetries=2, expected false")
	}
}

func TestHookTracksRetriesPerTurnStep(t *testing.T) {
	_, sleep := recordingSleeper()
	policy := NormalPolicy()
	policy.MaxRetries = 1
	hook := Hook(policy, Internals{Sleep: sleep, Random: fixedRandom(0.5)})

	failure := classifiedError{code: "SERVER"}
	if !hook(context.Background(), 1, 1, failure) {
		t.Fatal("turn 1/step 1 first retry: expected true")
	}
	if hook(context.Background(), 1, 1, failure) {
		t.Fatal("turn 1/step 1 second retry exceeds MaxRetries=1, expected false")
	}
	// A different (turn, step) key starts its own count from zero.
	if !hook(context.Background(), 1, 2, failure) {
		t.Fatal("turn 1/step 2 first retry: expected true even though step 1 is exhausted")
	}
}

func TestHookAlwaysModeIgnoresCodeAndNeverExhausts(t *testing.T) {
	_, sleep := recordingSleeper()
	hook := Hook(AlwaysPolicy(Backoff{InitialDelay: time.Millisecond, MaxDelay: time.Second, JitterRatio: 0}),
		Internals{Sleep: sleep, Random: fixedRandom(0.5)})

	failure := classifiedError{code: "INVALID_REQUEST"} // not in DefaultRetryableCodes
	for i := 0; i < 20; i++ {
		if !hook(context.Background(), 1, 1, failure) {
			t.Fatalf("always mode gave up on attempt %d", i)
		}
	}
}

func TestHookUnclassifiedErrorIsNotRetryableUnderNormalMode(t *testing.T) {
	_, sleep := recordingSleeper()
	hook := Hook(NormalPolicy(), Internals{Sleep: sleep, Random: fixedRandom(0.5)})

	if hook(context.Background(), 1, 1, errors.New("plain error, no classification")) {
		t.Fatal("expected an unclassified error to classify as code \"\", not in RetryableCodes")
	}
}

func TestHookHonorsInBoundsProviderDelayWithoutJitter(t *testing.T) {
	delays, sleep := recordingSleeper()
	hook := Hook(NormalPolicy(), Internals{Sleep: sleep, Random: fixedRandom(0)}) // random=0 would shrink local backoff, proving this path ignores it

	failure := classifiedError{code: "RATE_LIMIT", retryAfter: 3 * time.Second}
	if !hook(context.Background(), 1, 1, failure) {
		t.Fatal("expected retry")
	}
	if len(*delays) != 1 || (*delays)[0] != 3*time.Second {
		t.Fatalf("delays = %v, want [3s] (provider delay used verbatim)", *delays)
	}
}

func TestHookNormalModeGivesUpWhenProviderDelayExceedsMaxDelay(t *testing.T) {
	_, sleep := recordingSleeper()
	hook := Hook(NormalPolicy(), Internals{Sleep: sleep, Random: fixedRandom(0.5)})

	failure := classifiedError{code: "RATE_LIMIT", retryAfter: time.Hour} // way past NormalPolicy's 10s MaxDelay
	if hook(context.Background(), 1, 1, failure) {
		t.Fatal("expected normal mode to give up rather than honor an out-of-bounds provider delay")
	}
}

func TestHookAlwaysModeFallsBackToLocalDelayWhenProviderDelayExceedsMax(t *testing.T) {
	delays, sleep := recordingSleeper()
	backoff := Backoff{InitialDelay: 100 * time.Millisecond, MaxDelay: time.Second, JitterRatio: 0}
	hook := Hook(AlwaysPolicy(backoff), Internals{Sleep: sleep, Random: fixedRandom(0.5)})

	failure := classifiedError{code: "RATE_LIMIT", retryAfter: time.Hour}
	if !hook(context.Background(), 1, 1, failure) {
		t.Fatal("expected always mode to retry via local backoff, not give up")
	}
	if len(*delays) != 1 || (*delays)[0] != 100*time.Millisecond {
		t.Fatalf("delays = %v, want [100ms] (local backoff, retry 1, no jitter)", *delays)
	}
}

func TestLocalDelayExponentialGrowthCappedAtMaxDelay(t *testing.T) {
	b := Backoff{InitialDelay: 500 * time.Millisecond, MaxDelay: 10 * time.Second, JitterRatio: 0}
	noJitter := fixedRandom(0.5) // jitterRatio=0 makes the random sample irrelevant

	cases := []struct {
		retry int
		want  time.Duration
	}{
		{1, 500 * time.Millisecond},
		{2, time.Second},
		{3, 2 * time.Second},
		{4, 4 * time.Second},
		{5, 8 * time.Second},
		{6, 10 * time.Second}, // 16s would exceed MaxDelay, capped
		{20, 10 * time.Second},
	}
	for _, tc := range cases {
		got := localDelay(b, tc.retry, noJitter)
		if got != tc.want {
			t.Errorf("localDelay(retry=%d) = %v, want %v", tc.retry, got, tc.want)
		}
	}
}

func TestLocalDelayJitterStaysWithinConfiguredRatio(t *testing.T) {
	b := Backoff{InitialDelay: time.Second, MaxDelay: time.Minute, JitterRatio: 0.1}

	lo := localDelay(b, 1, fixedRandom(0))    // jitter factor 0.9
	hi := localDelay(b, 1, fixedRandom(1))    // jitter factor 1.1
	mid := localDelay(b, 1, fixedRandom(0.5)) // jitter factor 1.0

	if lo != 900*time.Millisecond {
		t.Errorf("lo = %v, want 900ms", lo)
	}
	if hi != 1100*time.Millisecond {
		t.Errorf("hi = %v, want 1.1s", hi)
	}
	if mid != time.Second {
		t.Errorf("mid = %v, want 1s", mid)
	}
}

func TestHookCancellationDuringWaitStopsRetrying(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the hook ever waits

	hook := Hook(NormalPolicy()) // real sleep + real random — must still return promptly since realSleep checks ctx first
	if hook(ctx, 1, 1, classifiedError{code: "SERVER"}) {
		t.Fatal("expected a cancelled context to stop the retry, not schedule another attempt")
	}
}

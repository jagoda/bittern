package auth

import (
	"net"
	"strings"
	"sync"
	"time"
)

// RateLimitPolicy bounds failed login and setup attempts. A short lockout is
// applied per remote address and across all addresses.
type RateLimitPolicy struct {
	PerAddress int
	Global     int
	Window     time.Duration
	Lockout    time.Duration
}

// DefaultRateLimitPolicy is deliberately small: an owner typing a wrong
// password a few times is not locked out for long, while a remote guessing
// script is stopped quickly.
func DefaultRateLimitPolicy() RateLimitPolicy {
	return RateLimitPolicy{
		PerAddress: 5,
		Global:     20,
		Window:     10 * time.Minute,
		Lockout:    5 * time.Minute,
	}
}

type rateState struct {
	windowStart time.Time
	failures    int
	lockedUntil time.Time
}

type rateLimiter struct {
	mu        sync.Mutex
	policy    RateLimitPolicy
	now       func() time.Time
	addresses map[string]*rateState
	global    rateState
}

func newRateLimiter(policy RateLimitPolicy, now func() time.Time) *rateLimiter {
	if policy.PerAddress <= 0 {
		policy.PerAddress = DefaultRateLimitPolicy().PerAddress
	}
	if policy.Global <= 0 {
		policy.Global = DefaultRateLimitPolicy().Global
	}
	if policy.Window <= 0 {
		policy.Window = DefaultRateLimitPolicy().Window
	}
	if policy.Lockout <= 0 {
		policy.Lockout = DefaultRateLimitPolicy().Lockout
	}
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{
		policy:    policy,
		now:       now,
		addresses: make(map[string]*rateState),
	}
}

func (l *rateLimiter) allow(address string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	state := l.addressState(address, now)
	l.resetWindow(&l.global, now)
	l.resetWindow(state, now)
	if l.global.lockedUntil.After(now) || state.lockedUntil.After(now) {
		return false
	}
	if l.global.failures >= l.policy.Global {
		l.lock(&l.global, now)
		return false
	}
	if state.failures >= l.policy.PerAddress {
		l.lock(state, now)
		return false
	}
	return true
}

func (l *rateLimiter) fail(address string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	state := l.addressState(address, now)
	l.resetWindow(&l.global, now)
	l.resetWindow(state, now)
	l.global.failures++
	state.failures++
	if l.global.failures >= l.policy.Global {
		l.lock(&l.global, now)
	}
	if state.failures >= l.policy.PerAddress {
		l.lock(state, now)
	}
}

func (l *rateLimiter) success(address string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.addresses, address)
	l.global.failures = 0
	l.global.lockedUntil = time.Time{}
}

func (l *rateLimiter) addressState(address string, now time.Time) *rateState {
	state := l.addresses[address]
	if state == nil {
		state = &rateState{windowStart: now}
		l.addresses[address] = state
	}
	return state
}

func (l *rateLimiter) resetWindow(state *rateState, now time.Time) {
	if !state.lockedUntil.IsZero() && !state.lockedUntil.After(now) {
		state.lockedUntil = time.Time{}
		state.windowStart = now
		state.failures = 0
		return
	}
	if state.windowStart.IsZero() {
		state.windowStart = now
		return
	}
	if now.Sub(state.windowStart) >= l.policy.Window {
		state.windowStart = now
		state.failures = 0
	}
}

func (l *rateLimiter) lock(state *rateState, now time.Time) {
	state.lockedUntil = now.Add(l.policy.Lockout)
}

func remoteAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	return value
}

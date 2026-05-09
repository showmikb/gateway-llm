package router

import (
	"sync/atomic"
	"time"
)

// CircuitState represents the three classical circuit-breaker states.
//
//	closed   -> normal operation; requests flow through
//	open     -> all requests short-circuit; used to shed load from a sick
//	             deployment and give it time to recover
//	halfOpen -> a single probe request is allowed; on success we close
//	             the breaker, on failure we re-open with backoff
type CircuitState int32

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// CircuitBreaker trips a deployment offline after consecutive failures
// exceed a threshold and keeps it offline for a cooldown window before
// admitting a single probe. All fields are accessed via atomics so the
// hot path (Allow / Report*) stays lock-free.
type CircuitBreaker struct {
	failureThreshold int32
	openDuration     time.Duration

	state        atomic.Int32
	failures     atomic.Int32
	openedAtUnix atomic.Int64
	// probeInFlight ensures only one request at a time is used as the
	// half-open probe; all others short-circuit.
	probeInFlight atomic.Bool
}

func NewCircuitBreaker(threshold int, openDuration time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 5
	}
	if openDuration <= 0 {
		openDuration = 30 * time.Second
	}
	return &CircuitBreaker{
		failureThreshold: int32(threshold),
		openDuration:     openDuration,
	}
}

// Allow returns true if the caller should attempt the request. When false
// the breaker is open and the deployment should be skipped.
func (cb *CircuitBreaker) Allow() bool {
	if cb == nil {
		return true
	}
	switch CircuitState(cb.state.Load()) {
	case CircuitClosed:
		return true
	case CircuitOpen:
		openedAt := cb.openedAtUnix.Load()
		if openedAt == 0 {
			return true
		}
		if time.Since(time.Unix(0, openedAt)) < cb.openDuration {
			return false
		}
		// Cooldown elapsed -> try to transition to half-open. Only the
		// goroutine that wins the CAS gets to send the probe.
		if cb.state.CompareAndSwap(int32(CircuitOpen), int32(CircuitHalfOpen)) {
			cb.probeInFlight.Store(true)
			return true
		}
		return cb.probeInFlight.CompareAndSwap(false, true)
	case CircuitHalfOpen:
		return cb.probeInFlight.CompareAndSwap(false, true)
	}
	return true
}

// ReportSuccess resets failure count; closes a half-open breaker.
func (cb *CircuitBreaker) ReportSuccess() {
	if cb == nil {
		return
	}
	cb.failures.Store(0)
	cb.state.Store(int32(CircuitClosed))
	cb.openedAtUnix.Store(0)
	cb.probeInFlight.Store(false)
}

// ReportFailure increments failure count; opens breaker at threshold. If
// currently half-open, a single failure reopens the breaker immediately.
func (cb *CircuitBreaker) ReportFailure() {
	if cb == nil {
		return
	}
	if CircuitState(cb.state.Load()) == CircuitHalfOpen {
		cb.state.Store(int32(CircuitOpen))
		cb.openedAtUnix.Store(time.Now().UnixNano())
		cb.probeInFlight.Store(false)
		return
	}

	n := cb.failures.Add(1)
	if n >= cb.failureThreshold {
		cb.state.Store(int32(CircuitOpen))
		cb.openedAtUnix.Store(time.Now().UnixNano())
	}
}

func (cb *CircuitBreaker) State() CircuitState {
	if cb == nil {
		return CircuitClosed
	}
	return CircuitState(cb.state.Load())
}

package polaris

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-lynx/lynx/log"
)

// RetryManager retry manager
// Provides exponential backoff retry mechanism
type RetryManager struct {
	maxRetries    int
	retryInterval time.Duration
	backoffFactor float64
}

// NewRetryManager creates new retry manager
func NewRetryManager(maxRetries int, retryInterval time.Duration) *RetryManager {
	return &RetryManager{
		maxRetries:    maxRetries,
		retryInterval: retryInterval,
		backoffFactor: 2.0, // Exponential backoff factor
	}
}

// DoWithRetry executes operation with retry
func (r *RetryManager) DoWithRetry(operation func() error) error {
	var lastErr error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		if err := operation(); err == nil {
			if attempt > 0 {
				log.Infof("Operation succeeded after %d retries", attempt)
			}
			return nil
		} else {
			lastErr = err
			if attempt < r.maxRetries {
				// Calculate backoff time
				backoffTime := r.calculateBackoff(attempt)
				log.Warnf("Operation failed (attempt %d/%d): %v, retrying in %v",
					attempt+1, r.maxRetries+1, err, backoffTime)
				time.Sleep(backoffTime)
			}
		}
	}

	return fmt.Errorf("operation failed after %d attempts, last error: %w", r.maxRetries+1, lastErr)
}

// DoWithRetryContext executes operation with retry (supports context)
func (r *RetryManager) DoWithRetryContext(ctx context.Context, operation func() error) error {
	var lastErr error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled: %w", ctx.Err())
		default:
		}

		if err := operation(); err == nil {
			if attempt > 0 {
				log.Infof("Operation succeeded after %d retries", attempt)
			}
			return nil
		} else {
			lastErr = err
			if attempt < r.maxRetries {
				backoffTime := r.calculateBackoff(attempt)
				log.Warnf("Operation failed (attempt %d/%d): %v, retrying in %v",
					attempt+1, r.maxRetries+1, err, backoffTime)

				select {
				case <-time.After(backoffTime):
				case <-ctx.Done():
					return fmt.Errorf("operation cancelled during retry: %w", ctx.Err())
				}
			}
		}
	}

	return fmt.Errorf("operation failed after %d attempts, last error: %w", r.maxRetries+1, lastErr)
}

// calculateBackoff calculates backoff time
func (r *RetryManager) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: base * factor^attempt
	backoffSeconds := float64(r.retryInterval) * math.Pow(r.backoffFactor, float64(attempt))

	// Limit maximum backoff time to 30 seconds
	maxBackoff := 30 * time.Second
	if time.Duration(backoffSeconds) > maxBackoff {
		return maxBackoff
	}

	return time.Duration(backoffSeconds)
}

// CircuitBreaker circuit breaker
// Implements simple circuit breaker protection mechanism
type CircuitBreaker struct {
	threshold        float64
	halfOpenTimeout  time.Duration
	failureCount     int
	successCount     int
	lastFailure      time.Time
	state            CircuitState
	halfOpenInFlight bool
	mu               sync.Mutex
}

// CircuitState circuit breaker state
type CircuitState int

const (
	CircuitStateClosed   CircuitState = iota // Closed state: normal
	CircuitStateOpen                         // Open state: circuit broken
	CircuitStateHalfOpen                     // Half-open state: attempting recovery
)

// NewCircuitBreaker creates new circuit breaker with configurable threshold and half-open timeout
func NewCircuitBreaker(threshold float64, halfOpenTimeout time.Duration) *CircuitBreaker {
	if halfOpenTimeout <= 0 {
		halfOpenTimeout = 30 * time.Second
	}
	return &CircuitBreaker{
		threshold:       threshold,
		halfOpenTimeout: halfOpenTimeout,
		state:           CircuitStateClosed,
	}
}

// Do executes operation with circuit breaker protection
func (cb *CircuitBreaker) Do(operation func() error) error {
	if err := cb.beforeRequest(); err != nil {
		return err
	}

	err := operation()
	cb.afterRequest(err)
	return err
}

func (cb *CircuitBreaker) beforeRequest() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitStateOpen:
		if time.Since(cb.lastFailure) > cb.halfOpenTimeout {
			cb.state = CircuitStateHalfOpen
			cb.halfOpenInFlight = true
			log.Infof("Circuit breaker transitioning to half-open state")
		} else {
			return fmt.Errorf("circuit breaker is open")
		}
	case CircuitStateHalfOpen:
		if cb.halfOpenInFlight {
			return fmt.Errorf("circuit breaker is half-open")
		}
		cb.halfOpenInFlight = true
		log.Infof("Circuit breaker in half-open state, allowing one attempt")
	case CircuitStateClosed:
	default:
		return fmt.Errorf("invalid circuit breaker state: %v", cb.state)
	}
	return nil
}

func (cb *CircuitBreaker) afterRequest(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == CircuitStateHalfOpen {
		cb.halfOpenInFlight = false
	}
	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}
}

// recordFailure records failure
func (cb *CircuitBreaker) recordFailure() {
	cb.failureCount++
	cb.lastFailure = time.Now()

	// Calculate failure rate
	failureRate := float64(cb.failureCount) / float64(cb.failureCount+cb.successCount)

	if cb.state == CircuitStateClosed && failureRate >= cb.threshold {
		cb.state = CircuitStateOpen
		log.Warnf("Circuit breaker opened: failure rate %.2f >= threshold %.2f",
			failureRate, cb.threshold)
	} else if cb.state == CircuitStateHalfOpen {
		cb.state = CircuitStateOpen
		log.Warnf("Circuit breaker reopened after failed attempt")
	}
}

// recordSuccess records success
func (cb *CircuitBreaker) recordSuccess() {
	cb.successCount++

	if cb.state == CircuitStateHalfOpen {
		// Success in half-open state, reset to closed state
		cb.state = CircuitStateClosed
		cb.resetCounters()
		log.Infof("Circuit breaker closed after successful attempt")
	}
}

// resetCounters resets counters
func (cb *CircuitBreaker) resetCounters() {
	cb.failureCount = 0
	cb.successCount = 0
}

// GetState gets circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// GetFailureRate gets failure rate
func (cb *CircuitBreaker) GetFailureRate() float64 {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	total := cb.failureCount + cb.successCount
	if total == 0 {
		return 0
	}
	return float64(cb.failureCount) / float64(total)
}

// ForceOpen forces circuit breaker to open
func (cb *CircuitBreaker) ForceOpen() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CircuitStateOpen
	cb.halfOpenInFlight = false
	log.Warnf("Circuit breaker forced open")
}

// ForceClose forces circuit breaker to close
func (cb *CircuitBreaker) ForceClose() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CircuitStateClosed
	cb.halfOpenInFlight = false
	cb.resetCounters()
	log.Infof("Circuit breaker forced closed")
}

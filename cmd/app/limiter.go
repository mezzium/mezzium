package main

type limiter struct {
	sem chan struct{}
}

func newLimiter(n int) *limiter {
	return &limiter{sem: make(chan struct{}, n)}
}

func (l *limiter) acquire() { l.sem <- struct{}{} }
func (l *limiter) release() { <-l.sem }

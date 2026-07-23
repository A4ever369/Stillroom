package main

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Anonymous publishing stays open, so the abuse story has to be answered
// somewhere other than a login wall.
//
// Requiring an account to publish would be the easy answer and the wrong one:
// the entire entry point of this product is that a person pastes one line and
// it works. A sign-up form in the middle of that is the difference between a
// tool someone tries and a tool someone bounces off. Trust in a shared pack
// comes from the channel it arrived through — a message from a colleague — not
// from the hub having checked anyone's identity.
//
// What an open endpoint genuinely risks is being used as free storage, and
// that is a volume problem, not an identity problem. So it gets a volume
// answer: a small hourly budget per address, generous enough that a real
// person sharing their work never meets it, small enough that the endpoint is
// not worth abusing. Signed-in accounts are exempt — at that point there is
// someone to hold responsible.

const (
	anonUploadsPerHour = 20
	rateWindow         = time.Hour
)

type rateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newRateLimiter() *rateLimiter { return &rateLimiter{hits: map[string][]time.Time{}} }

// allow records an attempt and reports whether it is within budget. It also
// returns how long until the next slot frees up, so the caller can say
// something more useful than "no".
func (r *rateLimiter) allow(key string, limit int) (bool, time.Duration) {
	now := time.Now()
	cutoff := now.Add(-rateWindow)

	r.mu.Lock()
	defer r.mu.Unlock()

	kept := r.hits[key][:0]
	for _, t := range r.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	r.hits[key] = kept

	// Opportunistic sweep so the map cannot grow without bound on a busy host.
	if len(r.hits) > 4096 {
		for k, ts := range r.hits {
			if len(ts) == 0 || ts[len(ts)-1].Before(cutoff) {
				delete(r.hits, k)
			}
		}
	}

	if len(kept) >= limit {
		return false, rateWindow - now.Sub(kept[0])
	}
	r.hits[key] = append(r.hits[key], now)
	return true, 0
}

// clientIP identifies the caller for rate limiting. X-Forwarded-For is trusted
// because the hub is designed to sit behind a reverse proxy that sets it; the
// leftmost entry is used, which a client can forge, so this is a courtesy
// bound on volume rather than a security boundary. It never has to be right,
// only mostly right.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return trimSpace(xff[:i])
			}
		}
		return trimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

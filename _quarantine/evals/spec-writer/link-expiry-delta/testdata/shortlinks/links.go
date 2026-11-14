package shortlinks

import (
	"errors"
	"time"
)

// defaultTTL is how long a link lives when it is created without an expiry.
const defaultTTL = 30 * 24 * time.Hour

// ErrExpired is returned for a link past its expiry; the HTTP layer maps it to 410.
var ErrExpired = errors.New("link expired")

// Link is one short code and its target.
type Link struct {
	Code      string
	Owner     string
	Target    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// NewLink creates a link; a zero expiresAt means the default lifetime.
func NewLink(code, owner, target string, now, expiresAt time.Time) Link {
	if expiresAt.IsZero() {
		expiresAt = now.Add(defaultTTL)
	}
	return Link{Code: code, Owner: owner, Target: target, CreatedAt: now, ExpiresAt: expiresAt}
}

// Resolve returns the target, or ErrExpired at or after the expiry instant.
func (l Link) Resolve(now time.Time) (string, error) {
	if !now.Before(l.ExpiresAt) {
		return "", ErrExpired
	}
	return l.Target, nil
}

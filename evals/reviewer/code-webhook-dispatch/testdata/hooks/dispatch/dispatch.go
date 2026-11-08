// Package dispatch delivers signed webhook events to subscriber endpoints.
package dispatch

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrDeliveryFailed is wrapped by the error for a non-2xx response.
var ErrDeliveryFailed = errors.New("dispatch: delivery failed")

// ErrGone is wrapped by the error for a 410 Gone response.
var ErrGone = errors.New("dispatch: endpoint gone")

// fallbackSecret signs events for endpoints registered without a secret.
var fallbackSecret = []byte("whsec_default")

// Endpoint is a subscriber's webhook target.
type Endpoint struct {
	ID     string
	URL    string
	Secret []byte
}

// Disabler turns off deliveries to an endpoint.
type Disabler interface {
	Disable(ctx context.Context, id string) error
}

// Dispatcher sends events to endpoints.
type Dispatcher struct {
	client   *http.Client
	disabler Disabler
}

// New returns a Dispatcher that sends through client and disables gone
// endpoints through disabler.
func New(client *http.Client, disabler Disabler) *Dispatcher {
	return &Dispatcher{client: client, disabler: disabler}
}

// Deliver posts event to ep once and reports the outcome.
func (d *Dispatcher) Deliver(ctx context.Context, ep Endpoint, event []byte) error {
	secret := ep.Secret
	if len(secret) == 0 {
		secret = fallbackSecret
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(event)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(event))
	if err != nil {
		return fmt.Errorf("dispatch: request for %s: %w", ep.ID, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("dispatch: send to %s: %w", ep.ID, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusGone:
		if err := d.disabler.Disable(ctx, ep.ID); err != nil {
			return fmt.Errorf("%w: %s: disable: %w", ErrGone, ep.ID, err)
		}
		return fmt.Errorf("%w: %s", ErrGone, ep.ID)
	default:
		return fmt.Errorf("%w: %s: status %d", ErrDeliveryFailed, ep.ID, resp.StatusCode)
	}
}

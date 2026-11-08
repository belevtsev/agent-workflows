package dispatch

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type noDisable struct{ t *testing.T }

func (n noDisable) Disable(context.Context, string) error {
	n.t.Error("Disable called")
	return nil
}

func sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// R1: a 2xx response is success, and the request carries the body, the
// content type and the signature.
func TestDeliverSignsAndSucceeds(t *testing.T) {
	event := []byte(`{"type":"invoice.paid"}`)
	secret := []byte("s3cret")
	var got struct {
		method, contentType, signature string
		body                           []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.contentType, got.signature = r.Method, r.Header.Get("Content-Type"), r.Header.Get("X-Signature")
		got.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	d := New(srv.Client(), noDisable{t})
	if err := d.Deliver(t.Context(), Endpoint{ID: "ep1", URL: srv.URL, Secret: secret}, event); err != nil {
		t.Fatalf("Deliver = %v, want nil", err)
	}
	if got.method != http.MethodPost || got.contentType != "application/json" || string(got.body) != string(event) {
		t.Fatalf("request = %s %q %q, want POST application/json %q", got.method, got.contentType, got.body, event)
	}
	if want := sign(secret, event); got.signature != want {
		t.Fatalf("X-Signature = %q, want %q", got.signature, want)
	}
}

// R2: a non-2xx status other than 410 fails with the status code.
func TestDeliverServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	d := New(srv.Client(), noDisable{t})
	err := d.Deliver(t.Context(), Endpoint{ID: "ep1", URL: srv.URL, Secret: []byte("k")}, []byte(`{}`))
	if !errors.Is(err, ErrDeliveryFailed) || !strings.Contains(err.Error(), "500") {
		t.Fatalf("Deliver = %v, want ErrDeliveryFailed with status 500", err)
	}
}

// R4: a transport failure is returned after one attempt.
func TestDeliverTransportError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()
	d := New(&http.Client{Timeout: 5 * time.Second}, noDisable{t})
	err := d.Deliver(t.Context(), Endpoint{ID: "ep1", URL: srv.URL, Secret: []byte("k")}, []byte(`{}`))
	if err == nil || errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("Deliver = %v, want a transport error", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("attempts = %d, want 1", n)
	}
}

// R6: an endpoint URL that cannot form a request fails without sending.
func TestDeliverBadURL(t *testing.T) {
	d := New(http.DefaultClient, noDisable{t})
	if err := d.Deliver(t.Context(), Endpoint{ID: "ep1", URL: "http://bad host/", Secret: []byte("k")}, []byte(`{}`)); err == nil {
		t.Fatal("Deliver to a malformed URL = nil, want an error")
	}
}

package workflowrunner

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClientBindsFreshnessSignatureAndJob(t *testing.T) {
	for _, changed := range []string{"", "request", "project", "job", "unfinished", "native-platforms"} {
		t.Run(changed, func(t *testing.T) {
			cfg := runnerServer(t, func(in Request, got *Receipt) {
				if changed == "native-platforms" && in.Action == "probe" {
					got.Capabilities.NativeTest = []string{"linux", "darwin"}
				}
				if in.Action != "start" {
					return
				}
				switch changed {
				case "request":
					got.Request = "different request"
				case "project":
					got.Project = "unregistered project"
				case "job":
					got.Job = "another job"
				case "unfinished":
					got.Stopped = false
				}
			})
			client, err := Connect(t.Context(), cfg)
			if changed == "native-platforms" {
				if err == nil || !strings.Contains(err.Error(), "native acceptance for windows") {
					t.Fatalf("accepted incomplete native acceptance: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			signed, got, err := client.Run(t.Context(), testJob(client))
			if changed != "" {
				if err == nil {
					t.Fatalf("accepted mismatched result: %+v", got)
				}
				return
			}
			if err != nil || !got.Stopped {
				t.Fatalf("%+v, %v", got, err)
			}
			signed.Signature[0] ^= 1
			if _, err := client.Verify(signed, got.Attempt, got.Binding); err == nil {
				t.Fatal("accepted a damaged receipt signature")
			}
		})
	}
}

func TestClientCancellationRequestsWholeJobStop(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped := make(chan Request, 1)
	cfg := runnerServer(t, func(in Request, got *Receipt) {
		switch in.Action {
		case "start":
			got.Status, got.Stopped = "running", false
			cancel()
		case "cancel":
			stopped <- in
		}
	})
	client, err := Connect(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	job := testJob(client)
	if _, _, err := client.Run(ctx, job); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel returned %v", err)
	}
	select {
	case req := <-stopped:
		if req.Attempt != job.Attempt || req.Binding != job.Binding {
			t.Fatalf("cancel targeted a different job: %+v", req)
		}
	default:
		t.Fatal("cancellation did not ask the runner to stop the reservation")
	}
}

func testJob(client *Client) Job {
	return Job{
		Task: "task", Attempt: "attempt", Binding: "candidate", Kind: "verify",
		Policy: Digest(client.Policy), Profile: client.Profile,
		Deadline: time.Now().Add(time.Minute),
	}
}

func runnerServer(t *testing.T, change func(Request, *Receipt)) Registration {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(out http.ResponseWriter, req *http.Request) {
		caps := Capabilities{Platform: runtime.GOOS + "/" + runtime.GOARCH, Profile: "validated",
			Policy: Policy{Declaration: "undeclared"}, NativeTest: []string{"linux", "darwin", "windows"}}
		data, err := io.ReadAll(req.Body)
		var in Request
		if err != nil || json.Unmarshal(data, &in) != nil {
			out.WriteHeader(http.StatusBadRequest)
			return
		}
		got := Receipt{
			Request: BytesDigest(data), Project: in.Project, Attempt: in.Attempt, Binding: in.Binding,
			Platform: caps.Platform, Profile: caps.Profile, Policy: Digest(caps.Policy),
			Status: "returned", Stopped: true,
		}
		if in.Action == "probe" {
			got.Status, got.Stopped, got.Capabilities = "ready", false, &caps
		}
		if in.Job != nil {
			got.Job = Digest(*in.Job)
		}
		change(in, &got)
		body, _ := json.Marshal(got)
		_ = json.NewEncoder(out).Encode(Signed{Receipt: body, Signature: ed25519.Sign(key, body)})
	}))
	t.Cleanup(server.Close)
	return Registration{Project: "demo", Endpoint: server.URL, ReceiptKey: pub,
		RootCA: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))}
}

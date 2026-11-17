package cli

import "testing"

// These are actual replay boundary payloads, not estimated provider tokens.
// canonical-cli is the documented word serialization; fake-native-* records
// are the JSON envelopes used by the deterministic in-process host below.
type replayTransport struct {
	Boundary string `json:"boundary"`
	Request  string `json:"request"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

func (m *workflowMeter) recordTransport(boundary, request, stdout, stderr string) {
	m.calls++
	m.requestBytes += int64(len(request))
	m.responseBytes += int64(len(stdout) + len(stderr))
	if m.retainTransports {
		m.transports = append(m.transports, replayTransport{boundary, request, stdout, stderr})
	}
}

func verifyReplayLedger(t testing.TB, m *workflowMeter) {
	t.Helper()
	var request, response int64
	for _, entry := range m.transports {
		if entry.Boundary == "" || entry.Request == "" {
			t.Fatal("transport ledger has an unnamed or empty request")
		}
		request += int64(len(entry.Request))
		response += int64(len(entry.Stdout) + len(entry.Stderr))
	}
	if int64(len(m.transports)) != m.calls || request != m.requestBytes || response != m.responseBytes {
		t.Fatalf("ledger mismatch: records=%d calls=%d bytes=%d/%d counters=%d/%d", len(m.transports), m.calls, request, response, m.requestBytes, m.responseBytes)
	}
}

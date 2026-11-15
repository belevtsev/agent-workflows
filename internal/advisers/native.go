package advisers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// NativeCall holds a prepared request and credentials only in local memory.
// Preparing a call performs no network I/O and writes no records.
type NativeCall struct {
	definition Definition
	request    Request
	body       []byte
	key        string
}

func PrepareNative(d Definition, r Request, prompt string) (*NativeCall, error) {
	if d.Provider == "" {
		return nil, errors.New("a native call requires a provider")
	}
	if err := validateDefinition(d); err != nil {
		return nil, err
	}
	if err := ValidateCapability(d.Provider, r); err != nil {
		return nil, err
	}
	if len(prompt) > MaxInput || !utf8.ValidString(prompt) || strings.TrimSpace(prompt) == "" {
		return nil, errors.New("native context must be nonempty UTF-8 text of at most 1 MiB")
	}
	key := os.Getenv(d.APIKeyEnv)
	if key == "" {
		return nil, fmt.Errorf("set %s to authenticate this adviser", d.APIKeyEnv)
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return nil, errors.New("adviser API key must contain only printable non-space ASCII")
		}
	}
	question := map[string]any{"instructions": r.Question, "type": r.Kind}
	switch r.Kind {
	case "decision":
		criteria := make(map[string]string, len(r.Choices))
		for _, c := range r.Choices {
			criteria[c.ID] = c.Text
		}
		question["type"], question["criteria"] = "choice", criteria
	case "score":
		question["criteria"] = r.Levels
	case "boolean":
		question["type"] = "noul"
		if r.Criteria != nil {
			question["criteria"] = r.Criteria
		}
	}
	body, err := json.Marshal(map[string]any{
		"model": d.Model, "state": prompt, "questions": map[string]any{"result": question},
	})
	if err != nil || len(body) > MaxInput {
		return nil, errors.New("encoded native request exceeds 1 MiB")
	}
	return &NativeCall{definition: d, request: r, body: body, key: key}, nil
}

// Execute performs one HTTP attempt. The optional client supplies a transport
// for local tests; redirects and cookies are disabled even on supplied clients.
func (call *NativeCall) Execute(ctx context.Context, client *http.Client) (result Execution) {
	result = Execution{Version: 2, Outcome: "failed", ActualModel: "unknown", StartedAt: time.Now().UTC()}
	defer func() { result.EndedAt = time.Now().UTC() }()
	ctx, cancel := context.WithTimeout(ctx, call.definition.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(call.definition.BaseURL, "/")+"/systemone", bytes.NewReader(call.body))
	if err != nil {
		result.Detail = "could not prepare adviser HTTP request"
		return result
	}
	req.Header.Set("Authorization", "Bearer "+call.key)
	req.Header.Set("Content-Type", "application/json")
	var httpClient http.Client
	if client != nil {
		httpClient = *client
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	httpClient.Jar = nil
	response, err := httpClient.Do(req)
	if err != nil {
		result.Outcome, result.Detail = nativeTransportFailure(ctx)
		return result
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		result.Detail = nativeHTTPError(response.StatusCode)
		return result
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxAnswer+1))
	if err != nil {
		result.Outcome, result.Detail = nativeTransportFailure(ctx)
		return result
	}
	if ctx.Err() != nil {
		result.Outcome, result.Detail = nativeTransportFailure(ctx)
		return result
	}
	if len(body) > MaxAnswer {
		result.Outcome, result.Detail = "overflow", "adviser response exceeded 1 MiB"
		return result
	}
	if responseContainsCredential(body, call.key) {
		result.Detail = "adviser response contained credentials and was discarded"
		return result
	}
	answer, model, usage, err := decodeNativeResponse(body, call.request)
	if err != nil {
		result.Detail = "adviser returned an invalid structured response"
		return result
	}
	result.Answer, result.Result, result.ActualModel, result.Usage = string(body), answer, model, usage
	result.UsageKnown, result.Outcome = usage != nil, "success"
	return result
}

func responseContainsCredential(body []byte, key string) bool {
	if bytes.Contains(body, []byte(key)) {
		return true
	}
	var value any
	if json.Unmarshal(body, &value) != nil {
		return false
	}
	return containsCredential(value, key)
}

func containsCredential(value any, key string) bool {
	switch value := value.(type) {
	case string:
		return strings.Contains(value, key)
	case []any:
		for _, child := range value {
			if containsCredential(child, key) {
				return true
			}
		}
	case map[string]any:
		for name, child := range value {
			if strings.Contains(name, key) || containsCredential(child, key) {
				return true
			}
		}
	}
	return false
}

func nativeTransportFailure(ctx context.Context) (string, string) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timed_out", "adviser call exceeded its timeout"
	}
	if ctx.Err() != nil {
		return "cancelled", "adviser call was cancelled"
	}
	return "failed", "adviser HTTP request failed"
}

func nativeHTTPError(status int) string {
	detail := "provider error"
	switch status {
	case 400, 422:
		detail = "provider rejected the request or context limit"
	case 401:
		detail = "provider rejected the API key"
	case 402:
		detail = "provider account has insufficient credits"
	case 403:
		detail = "provider denied access to the model"
	case 429:
		detail = "provider rate limit exceeded"
	default:
		if status >= 300 && status < 400 {
			detail = "provider redirect refused"
		}
	}
	return fmt.Sprintf("adviser HTTP %d: %s", status, detail)
}

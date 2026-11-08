package usagelog

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"
	"unicode"

	json "encoding/json/v2"

	"github.com/belevtsev/agent-workflows/v3/internal/outcomes"
)

// PriceTable is an explicitly supplied rate table in USD per million tokens.
// A missing rate is unknown, never zero.
type PriceTable struct {
	Currency string                `json:"currency"`
	Evidence string                `json:"evidence"`
	Models   map[string]ModelRates `json:"models"`
}

// ModelRates are per-million-token rates for one model id. For Codex, cached
// input uses CacheReadPerMillion and cache writes (which carry no TTL) use
// CacheWrite5mPerMillion.
type ModelRates struct {
	InputPerMillion        *float64 `json:"input_per_million,omitempty"`
	OutputPerMillion       *float64 `json:"output_per_million,omitempty"`
	CacheWrite5mPerMillion *float64 `json:"cache_write_5m_per_million,omitempty"`
	CacheWrite1hPerMillion *float64 `json:"cache_write_1h_per_million,omitempty"`
	CacheReadPerMillion    *float64 `json:"cache_read_per_million,omitempty"`
}

// LoadPriceTable reads and validates a price table file of at most
// MaxSmallFileBytes.
func LoadPriceTable(path string) (PriceTable, error) {
	data, err := readSmallFile(path)
	if err != nil {
		return PriceTable{}, fmt.Errorf("read price table: %w", err)
	}
	return ParsePriceTable(data)
}

// ParsePriceTable strictly decodes and validates a price table: unknown
// fields are rejected, currency must be USD, evidence must be non-blank, at
// least one model is required, and every supplied rate must be finite,
// nonnegative and within the outcomes metric limit.
func ParsePriceTable(data []byte) (PriceTable, error) {
	var table PriceTable
	if err := json.Unmarshal(data, &table, json.RejectUnknownMembers(true)); err != nil {
		return PriceTable{}, fmt.Errorf("invalid price table: %w", err)
	}
	if table.Currency != "USD" {
		return PriceTable{}, errors.New("invalid price table: currency must be USD")
	}
	if strings.TrimSpace(table.Evidence) == "" || len(table.Evidence) > 1024 || strings.ContainsFunc(table.Evidence, unicode.IsControl) {
		return PriceTable{}, errors.New("invalid price table: evidence must be non-blank bounded text naming the source of the rates")
	}
	if len(table.Models) == 0 {
		return PriceTable{}, errors.New("invalid price table: models must contain at least one model")
	}
	for _, model := range sortedKeys(table.Models) {
		rates := table.Models[model]
		if strings.TrimSpace(model) == "" || len(model) > 256 || strings.ContainsFunc(model, unicode.IsControl) {
			return PriceTable{}, errors.New("invalid price table: model ids must be non-blank bounded text")
		}
		for _, named := range rates.named() {
			name, rate := named.name, named.rate
			if rate != nil && (math.IsNaN(*rate) || math.IsInf(*rate, 0) || *rate < 0 || *rate > outcomes.MaxMetric) {
				return PriceTable{}, fmt.Errorf("invalid price table: %s for %s must be finite and nonnegative", name, model)
			}
		}
	}
	return table, nil
}

type namedRate struct {
	name string
	rate *float64
}

func (rates ModelRates) named() []namedRate {
	return []namedRate{
		{"input_per_million", rates.InputPerMillion}, {"output_per_million", rates.OutputPerMillion},
		{"cache_write_5m_per_million", rates.CacheWrite5mPerMillion}, {"cache_write_1h_per_million", rates.CacheWrite1hPerMillion},
		{"cache_read_per_million", rates.CacheReadPerMillion},
	}
}

func readSmallFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxSmallFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSmallFileBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", MaxSmallFileBytes)
	}
	return data, nil
}

// price computes the price-table amount for one model's tokens. Any nonzero
// category without a usable rate makes the amount unknown, never zero.
func (table *PriceTable) price(model, source string, tokens Tokens) Amount {
	if table == nil {
		return Amount{UnknownReason: "no --prices table supplied"}
	}
	rates, ok := table.Models[model]
	if !ok {
		return Amount{UnknownReason: fmt.Sprintf("price table has no rates for model %q", model)}
	}
	type part struct {
		name   string
		tokens int64
		rate   *float64
	}
	parts := []part{
		{"input_per_million", tokens.UncachedInputTokens, rates.InputPerMillion},
		{"output_per_million", tokens.OutputTokens, rates.OutputPerMillion},
		{"cache_read_per_million", tokens.CacheReadTokens, rates.CacheReadPerMillion},
		{"cache_write_5m_per_million", tokens.CacheWrite5mTokens, rates.CacheWrite5mPerMillion},
		{"cache_write_1h_per_million", tokens.CacheWrite1hTokens, rates.CacheWrite1hPerMillion},
	}
	if tokens.CacheWriteUnsplitTokens > 0 {
		switch {
		case source == SourceCodex:
			// Codex cache writes carry no TTL; the 5m rate is the standard write rate.
			parts = append(parts, part{"cache_write_5m_per_million", tokens.CacheWriteUnsplitTokens, rates.CacheWrite5mPerMillion})
		case rates.CacheWrite5mPerMillion != nil && rates.CacheWrite1hPerMillion != nil && *rates.CacheWrite5mPerMillion == *rates.CacheWrite1hPerMillion:
			parts = append(parts, part{"cache_write_5m_per_million", tokens.CacheWriteUnsplitTokens, rates.CacheWrite5mPerMillion})
		default:
			return Amount{UnknownReason: fmt.Sprintf("model %q has cache writes without a 5m/1h split and the price table does not give one equal rate for both", model)}
		}
	}
	total := 0.0
	for _, p := range parts {
		if p.tokens == 0 {
			continue
		}
		if p.rate == nil {
			return Amount{UnknownReason: fmt.Sprintf("price table lacks %s for model %q, which has nonzero tokens in that category", p.name, model)}
		}
		total += float64(p.tokens) / 1e6 * *p.rate
	}
	return Amount{Amount: &total, Basis: BasisPriceTable}
}

// sumAmounts adds known amounts; any unknown amount makes the sum unknown.
func sumAmounts(amounts []Amount, basis string) Amount {
	total := 0.0
	for _, amount := range amounts {
		if amount.Amount == nil {
			return Amount{UnknownReason: amount.UnknownReason}
		}
		total += *amount.Amount
	}
	return Amount{Amount: &total, Basis: basis}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

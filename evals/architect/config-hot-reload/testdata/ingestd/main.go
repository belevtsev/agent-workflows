package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

type Config struct {
	Version   int           `json:"version"`
	BatchSize int           `json:"batch_size"`
	Interval  time.Duration `json:"interval"`
}

func load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	cfg, err := load(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": cfg.Version})
	})
	go func() { log.Fatal(http.ListenAndServe(":8080", nil)) }()
	runBatches(context.Background(), cfg)
}

// runBatches reads cfg on every iteration; a batch can take up to 30 seconds.
func runBatches(ctx context.Context, cfg *Config) {
	for {
		processBatch(ctx, cfg.BatchSize)
		time.Sleep(cfg.Interval)
	}
}

func processBatch(ctx context.Context, size int) {}

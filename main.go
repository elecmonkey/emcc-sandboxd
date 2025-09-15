package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/exec"

	"emcc-sandboxd/src"
)

func main() {
	// Optional config.json in working directory
	cfg, err := src.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	// Validate minimal external deps when nsjail enabled
	if cfg.NsJailEnabled {
		if _, err := exec.LookPath(cfg.NsJailPath); err != nil {
			log.Fatalf("nsjail enabled but not found at '%s'", cfg.NsJailPath)
		}
	}
	if err := src.ValidateDirs(cfg); err != nil {
		log.Fatalf("invalid dirs: %v", err)
	}
	srv := src.NewServer(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server: %v", err)
	}
}

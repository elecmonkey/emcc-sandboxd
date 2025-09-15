package src

import (
	"os"
	"path/filepath"
	"time"
)

// StartCleanupLoop starts the background cleanup process for artifacts
func (s *Server) StartCleanupLoop() {
	dir := filepath.Join(s.cfg.BaseDir, s.cfg.ArtifactsDir)
	if s.cfg.ArtifactTTL <= 0 {
		s.cfg.ArtifactTTL = time.Duration(s.cfg.ArtifactTTLDays) * 24 * time.Hour
	}
	interval := time.Duration(s.cfg.CleanupIntervalMins) * time.Minute
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	ttl := s.cfg.ArtifactTTL
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			entries, err := os.ReadDir(dir)
			if err == nil {
				for _, e := range entries {
					fi, err := os.Stat(filepath.Join(dir, e.Name()))
					if err != nil || !fi.IsDir() {
						continue
					}
					if time.Since(fi.ModTime()) > ttl {
						_ = os.RemoveAll(filepath.Join(dir, e.Name()))
					}
				}
			}
			<-ticker.C
		}
	}()
}
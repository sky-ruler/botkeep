package gitops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"botkeep/internal/models"
)

type FleetConfig struct {
	Version string        `yaml:"version"`
	Targets []FleetTarget `yaml:"targets"`
}

type FleetTarget struct {
	Name           string `yaml:"name"`
	Driver         string `yaml:"driver"`
	URL            string `yaml:"url"`
	Protocol       string `yaml:"protocol,omitempty"`
	Region         string `yaml:"region,omitempty"`
	GRPCService    string `yaml:"grpc_service,omitempty"`
	RunbookURL     string `yaml:"runbook_url,omitempty"`
	RunbookEnabled bool   `yaml:"runbook_enabled,omitempty"`
	Public         *bool  `yaml:"public,omitempty"`
}

// LoadFleetConfig parses a YAML file from disk into FleetConfig.
func LoadFleetConfig(path string) (*FleetConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read fleet file: %w", err)
	}

	var cfg FleetConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse fleet yaml: %w", err)
	}

	if cfg.Version == "" {
		cfg.Version = "2.0"
	}

	return &cfg, nil
}

// ExportFleetConfig writes the active DB server inventory to formatted YAML bytes.
func ExportFleetConfig(db *models.DB) ([]byte, error) {
	servers, err := db.GetAllServers()
	if err != nil {
		return nil, err
	}

	cfg := FleetConfig{
		Version: "2.0",
		Targets: make([]FleetTarget, 0, len(servers)),
	}

	for _, s := range servers {
		isPublic := s.IsPublic
		cfg.Targets = append(cfg.Targets, FleetTarget{
			Name:           s.Name,
			Driver:         s.Driver,
			URL:            s.EndpointURL,
			Protocol:       s.Protocol,
			Region:         s.Region,
			GRPCService:    s.GRPCService,
			RunbookURL:     s.RunbookURL,
			RunbookEnabled: s.RunbookEnabled,
			Public:         &isPublic,
		})
	}

	return yaml.Marshal(cfg)
}

// ReconcileFleet syncs targets declared in FleetConfig with the embedded database.
func ReconcileFleet(db *models.DB, cfg *FleetConfig) (createdCount int, updatedCount int, err error) {
	existingServers, err := db.GetAllServers()
	if err != nil {
		return 0, 0, err
	}

	byName := make(map[string]models.Server)
	for _, s := range existingServers {
		byName[strings.ToLower(s.Name)] = s
	}

	for _, t := range cfg.Targets {
		tName := strings.TrimSpace(t.Name)
		if tName == "" {
			continue
		}

		driver := strings.ToLower(strings.TrimSpace(t.Driver))
		if driver == "" {
			driver = "http"
		}
		region := strings.TrimSpace(t.Region)
		if region == "" {
			region = "us-east-1"
		}

		if existing, found := byName[strings.ToLower(tName)]; found {
			// Update existing node settings
			if t.RunbookURL != "" || t.RunbookEnabled != existing.RunbookEnabled {
				_ = db.UpdateServerRunbook(existing.ID, t.RunbookURL, t.RunbookEnabled)
			}
			updatedCount++
		} else {
			// Provision newly declared target
			srv, err := db.CreateServer(tName, region, driver, t.URL)
			if err != nil {
				log.Printf("[GitOps] Failed to provision declared target %s: %v", tName, err)
				continue
			}
			if t.RunbookURL != "" {
				_ = db.UpdateServerRunbook(srv.ID, t.RunbookURL, t.RunbookEnabled)
			}
			createdCount++
		}
	}

	log.Printf("[GitOps] Fleet reconciled successfully: %d targets provisioned, %d updated", createdCount, updatedCount)
	return createdCount, updatedCount, nil
}

// WatchFleetFile polls the fleet config file and automatically reconciles when modified.
func WatchFleetFile(ctx context.Context, db *models.DB, path string, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}

	var lastHash string
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[GitOps] Watcher stopped.")
			return
		case <-ticker.C:
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			h := sha256.Sum256(data)
			currHash := hex.EncodeToString(h[:])
			if currHash != lastHash {
				if lastHash != "" {
					log.Printf("[GitOps] File change detected in %s. Reconciling fleet...", path)
				}
				lastHash = currHash

				cfg, err := LoadFleetConfig(path)
				if err == nil {
					_, _, _ = ReconcileFleet(db, cfg)
				} else {
					log.Printf("[GitOps] Failed to parse updated fleet yaml: %v", err)
				}
			}
		}
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"botkeep/internal/models"
)

func runSatelliteAgent(args []string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	hubURL := fs.String("hub", "http://localhost:8080", "BotKeep Central Hub URL")
	agentID := fs.String("id", "", "Unique Satellite Agent ID (default: sat-<region>-<pid>)")
	region := fs.String("region", "us-east-1", "Geographic region (e.g., us-east-1, eu-central-1, ap-northeast-1)")
	name := fs.String("name", "", "Human-readable satellite display name")
	interval := fs.Duration("interval", 15*time.Second, "Telemetry polling interval")
	_ = fs.Parse(args)

	if *agentID == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "node"
		}
		*agentID = fmt.Sprintf("sat-%s-%s", strings.ToLower((*region)[:2]), strings.ToLower(hostname))
	}
	if *name == "" {
		*name = fmt.Sprintf("Satellite Edge (%s)", *region)
	}

	cleanHub := strings.TrimRight(*hubURL, "/")

	log.Printf("[Satellite] Starting BotKeep Edge Satellite Agent v%s (%s)", version, commit)
	log.Printf("[Satellite] Agent ID: %s | Region: %s | Hub: %s", *agentID, *region, cleanHub)

	client := &http.Client{Timeout: 8 * time.Second}

	// Register initial heartbeat
	sendHeartbeat(client, cleanHub, *agentID, *name, *region)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Satellite] Agent shutting down gracefully.")
			return
		case <-ticker.C:
			// 1. Send periodic heartbeat
			sendHeartbeat(client, cleanHub, *agentID, *name, *region)

			// 2. Fetch targets from Hub
			targets := fetchTargets(client, cleanHub)
			if len(targets) == 0 {
				continue
			}

			// 3. Probe targets concurrently
			probes := probeTargetsConcurrently(targets, *agentID, *region)

			// 4. Report back to Hub
			sendProbeReport(client, cleanHub, *agentID, *region, probes)
		}
	}
}

type probeTarget struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Driver      string `json:"driver"`
	EndpointURL string `json:"endpoint_url"`
	Protocol    string `json:"protocol"`
	Region      string `json:"region"`
}

func sendHeartbeat(client *http.Client, hub, id, name, region string) {
	start := time.Now()
	payload, _ := json.Marshal(models.Satellite{
		ID:             id,
		Name:           name,
		Region:         region,
		LatencyToHubMS: 0,
		Version:        version,
		Status:         "online",
	})

	resp, err := client.Post(hub+"/api/satellite/heartbeat", "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("[Satellite] Heartbeat failed to Hub (%s): %v", hub, err)
		return
	}
	defer resp.Body.Close()
	lat := float64(time.Since(start).Microseconds()) / 1000.0
	log.Printf("[Satellite] Heartbeat confirmed by Hub (roundtrip: %.2fms)", lat)
}

func fetchTargets(client *http.Client, hub string) []probeTarget {
	resp, err := client.Get(hub + "/api/satellite/targets")
	if err != nil {
		log.Printf("[Satellite] Failed to fetch targets from Hub: %v", err)
		return nil
	}
	defer resp.Body.Close()

	var data struct {
		Targets []probeTarget `json:"targets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Printf("[Satellite] Failed to decode targets JSON: %v", err)
		return nil
	}
	return data.Targets
}

func probeTargetsConcurrently(targets []probeTarget, agentID, region string) []models.SatelliteProbe {
	results := make([]models.SatelliteProbe, 0, len(targets))
	for _, t := range targets {
		p := models.SatelliteProbe{
			SatelliteID: agentID,
			ServerID:    t.ID,
			Region:      region,
			CreatedAt:   time.Now(),
		}

		if t.EndpointURL == "" {
			continue
		}

		start := time.Now()
		if strings.EqualFold(t.Protocol, "TCP") {
			conn, err := net.DialTimeout("tcp", t.EndpointURL, 4*time.Second)
			lat := float64(time.Since(start).Microseconds()) / 1000.0
			p.LatencyMS = lat
			if err != nil {
				p.Success = false
				p.StatusCode = 0
				p.ErrorMessage = err.Error()
			} else {
				conn.Close()
				p.Success = true
				p.StatusCode = 200
			}
		} else {
			// HTTP/HTTPS probe
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(t.EndpointURL)
			lat := float64(time.Since(start).Microseconds()) / 1000.0
			p.LatencyMS = lat
			if err != nil {
				p.Success = false
				p.StatusCode = 0
				p.ErrorMessage = err.Error()
			} else {
				resp.Body.Close()
				p.StatusCode = resp.StatusCode
				p.Success = resp.StatusCode >= 200 && resp.StatusCode < 400
			}
		}
		results = append(results, p)
	}
	return results
}

func sendProbeReport(client *http.Client, hub, agentID, region string, probes []models.SatelliteProbe) {
	if len(probes) == 0 {
		return
	}

	payload, _ := json.Marshal(map[string]any{
		"satellite_id": agentID,
		"region":       region,
		"probes":       probes,
	})

	resp, err := client.Post(hub+"/api/satellite/report", "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("[Satellite] Failed to dispatch probe report to Hub: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("[Satellite] Dispatched %d edge probe observations to Hub", len(probes))
}

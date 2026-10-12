package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"botkeep"
	"botkeep/internal/gitops"
	"botkeep/internal/handlers"
	"botkeep/internal/models"
	"botkeep/internal/monitor"
)

var (
	version = "2.0.0"
	commit  = "release"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "agent" {
		runSatelliteAgent(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "export-fleet" {
		runExportFleet(os.Args[2:])
		return
	}

	flagPort := flag.String("port", "", "Server HTTP listening port (default: 8080 or $PORT)")
	flagDB := flag.String("db", "", "SQLite database filepath (default: botkeep.db or $DB_PATH)")
	flagInterval := flag.Duration("interval", 15*time.Second, "Health probe interval (default: 15s)")
	flagFleet := flag.String("fleet", "", "Path to declarative GitOps fleet.yaml configuration (optional)")
	flagVersion := flag.Bool("version", false, "Print BotKeep version information and exit")
	flagSeed := flag.Bool("seed", false, "Force re-seed demo server fleet on startup")
	flag.Parse()

	if *flagVersion {
		fmt.Printf("BotKeep DevOps Fleet Manager v%s (%s)\n", version, commit)
		fmt.Println("Built with Go, HTMX, templ, Tailwind CSS & SQLite")
		os.Exit(0)
	}

	port := *flagPort
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}

	dbPath := *flagDB
	if dbPath == "" {
		dbPath = os.Getenv("DB_PATH")
	}
	if dbPath == "" {
		dbPath = "botkeep.db"
	}

	log.Printf("[Init] Starting BotKeep Single-Binary Engine v%s on port :%s", version, port)
	log.Printf("[Init] Initializing embedded SQLite database at %s", dbPath)

	// Initialize SQLite with WAL pragmas & embedded schema
	db, err := models.NewDB(dbPath)
	if err != nil {
		log.Fatalf("[Fatal] Failed to initialize SQLite storage: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("[Warn] Error closing SQLite database: %v", err)
		}
	}()

	if *flagSeed {
		log.Println("[Init] -seed flag specified: seeding demo fleet instances")
		if err := db.SeedDemoFleet(); err != nil {
			log.Printf("[Warn] SeedDemoFleet: %v", err)
		}
	}

	// Initialize Chi Router
	r := chi.NewRouter()

	// High-performance middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	// Serve embedded static files (Tailwind CSS, HTMX script) directly from binary
	staticSubFS, err := fs.Sub(botkeep.StaticFS, "static")
	if err != nil {
		log.Fatalf("[Fatal] Failed to create embedded static sub-filesystem: %v", err)
	}

	fileServer := http.FileServer(http.FS(staticSubFS))
	r.Get("/static/*", func(w http.ResponseWriter, r *http.Request) {
		// Set caching headers for production performance
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.StripPrefix("/static/", fileServer).ServeHTTP(w, r)
	})

	// Start Background Fleet & Endpoint Health Prober
	mon := monitor.NewEngine(db, *flagInterval)
	monCtx, cancelMon := context.WithCancel(context.Background())
	defer cancelMon()
	go mon.Start(monCtx)

	// GitOps Declarative Fleet Reconciliation & Active File Watcher
	fleetPath := *flagFleet
	if fleetPath == "" {
		fleetPath = os.Getenv("BOTKEEP_FLEET_FILE")
	}
	if fleetPath != "" {
		log.Printf("[GitOps] Loading declarative fleet configuration from %s", fleetPath)
		if cfg, err := gitops.LoadFleetConfig(fleetPath); err == nil {
			_, _, _ = gitops.ReconcileFleet(db, cfg)
			go gitops.WatchFleetFile(monCtx, db, fleetPath, 5*time.Second)
		} else {
			log.Printf("[GitOps] Warning: failed to load %s: %v", fleetPath, err)
		}
	}

	// Handlers
	h := handlers.NewHandler(db, mon)

	// Application routes
	r.Get("/", h.Dashboard)
	r.Get("/servers", h.GetServers)
	r.Post("/servers/new", h.CreateServer)
	r.Post("/servers/clear", h.ClearFleet)
	r.Post("/servers/simulate-error", h.SimulateError)
	r.Get("/servers/{id}/details", h.GetServerDetails)
	r.Delete("/servers/{id}", h.DeleteServer)
	r.Post("/servers/{id}/delete", h.DeleteServer)
	r.Post("/servers/{id}/start", h.StartServer)
	r.Post("/servers/{id}/stop", h.StopServer)
	r.Post("/servers/{id}/restart", h.RestartServer)
	r.Post("/servers/{id}/ping", h.PingServer)
	r.Post("/servers/seed", h.SeedFleet)
	r.Get("/metrics", h.GetMetrics)
	r.Get("/metrics/prometheus", h.PrometheusMetrics)
	r.Get("/api/fleet/export", h.ExportFleet)
	r.Get("/logs", h.GetLogs)
	r.Post("/logs/clear", h.ClearLogs)

	// Public Status Page & Programmatic Feeds
	r.Get("/status", h.StatusPage)
	r.Get("/status/components", h.StatusComponents)
	r.Get("/api/status", h.StatusJSON)
	r.Post("/admin/incidents/new", h.CreateIncident)

	// Multi-Channel Alert & Notification Management
	r.Get("/notifications", h.NotificationsPage)
	r.Get("/notifications/list", h.NotificationChannelsListPartial)
	r.Post("/admin/notifications/new", h.CreateNotificationChannel)
	r.Post("/admin/notifications/{id}/toggle", h.ToggleNotificationChannel)
	r.Delete("/admin/notifications/{id}", h.DeleteNotificationChannel)
	r.Post("/admin/notifications/{id}/delete", h.DeleteNotificationChannel)
	r.Post("/admin/notifications/{id}/test", h.TestNotificationChannel)
	r.Post("/admin/notifications/test-all", h.TestAllNotifications)
	r.Get("/api/notifications", h.NotificationsJSON)

	// Zero-Config Satellite Mesh & Multi-Region Consensus API
	r.Get("/api/satellites", h.SatellitesJSON)
	r.Post("/api/satellite/heartbeat", h.SatelliteHeartbeat)
	r.Post("/api/satellite/report", h.SatelliteReport)
	r.Get("/api/satellite/targets", h.SatelliteTargets)

	// Self-Healing Runbooks & GitOps Endpoints
	r.Post("/servers/{id}/runbook", h.UpdateServerRunbook)
	r.Post("/servers/{id}/runbook/trigger", h.TriggerServerRunbook)
	r.Get("/api/fleet/gitops/export", h.ExportFleetYAML)

	// Health check endpoint
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 20 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Server startup banner
	fmt.Println("==================================================================")
	fmt.Println("  ⚡ BOTKEEP DEVOPS SERVER MANAGEMENT DASHBOARD")
	fmt.Println("  Runtime: Ultra-lean single-binary executable")
	fmt.Println("  Architecture: Go 1.23 + HTMX 2.0 + templ + Tailwind CSS + SQLite")
	fmt.Printf("  Dashboard URL: http://localhost:%s\n", port)
	fmt.Println("==================================================================")

	// Start server in background goroutine
	serverErrors := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	// Graceful shutdown handling
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		log.Fatalf("[Fatal] Server failed to start: %v", err)
	case sig := <-shutdown:
		log.Printf("[Shutdown] Signal %v received. Commencing graceful shutdown...", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("[Shutdown] Graceful shutdown timed out, forcing exit: %v", err)
			_ = srv.Close()
		}
		log.Println("[Shutdown] Server gracefully terminated.")
	}
}

func runExportFleet(args []string) {
	fs := flag.NewFlagSet("export-fleet", flag.ExitOnError)
	dbPath := fs.String("db", "botkeep.db", "SQLite database path")
	outFile := fs.String("out", "fleet.yaml", "Output filepath for exported fleet YAML")
	_ = fs.Parse(args)

	db, err := models.NewDB(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	data, err := gitops.ExportFleetConfig(db)
	if err != nil {
		log.Fatalf("Failed to export fleet YAML: %v", err)
	}

	if err := os.WriteFile(*outFile, data, 0644); err != nil {
		log.Fatalf("Failed to write %s: %v", *outFile, err)
	}
	fmt.Printf("Successfully exported fleet configuration to %s\n", *outFile)
}

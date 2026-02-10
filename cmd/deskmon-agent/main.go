package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/neur0map/deskmon-agent/internal/api"
	"github.com/neur0map/deskmon-agent/internal/collector"
	"github.com/neur0map/deskmon-agent/internal/collector/services"
	"github.com/neur0map/deskmon-agent/internal/config"
)

var Version = "dev"

func main() {
	configPath := flag.String("config", config.DefaultConfigPath, "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("deskmon-agent", Version)
		os.Exit(0)
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("deskmon-agent %s starting", Version)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	if cfg.AuthToken == "" {
		log.Println("WARNING: no auth_token configured — all authenticated endpoints will return 401")
	}

	// Initialize collectors
	systemCollector := collector.NewSystemCollector()
	systemCollector.Start()
	defer systemCollector.Stop()

	dockerCollector := collector.NewDockerCollector(config.DefaultDockerSock)
	dockerCollector.Start()
	defer dockerCollector.Stop()

	// Initialize service detector (auto-discovers Pi-hole, Traefik, etc.)
	svcDetector := services.NewServiceDetector(config.DefaultDockerSock)

	// Inject service credentials from config
	if cfg.Services.PiHole.Password != "" {
		svcDetector.SetServiceConfig("pihole", "password", cfg.Services.PiHole.Password)
		log.Println("pihole password loaded from config")
	}

	svcDetector.Start()
	defer svcDetector.Stop()

	log.Printf("listening on %s:%d", cfg.Bind, cfg.Port)

	// Start HTTP server
	srv := api.NewServer(cfg, systemCollector, dockerCollector, svcDetector, Version, *configPath)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("shutting down")
		_ = srv.Shutdown()
	}()

	if err := srv.Start(); err != nil {
		log.Printf("server stopped: %v", err)
	}
}

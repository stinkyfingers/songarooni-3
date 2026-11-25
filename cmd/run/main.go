package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"songarooni/config"
	control_panel "songarooni/control-panel"
	"songarooni/data"
)

var (
	configPath = flag.String("config", "config.yaml", "path to config.yaml")
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flag.Parse()

	// config
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "songarooni:", err)
		os.Exit(1)
	}

	// logs
	logFile, err := os.Create(cfg.LogFile) // truncates on every startup, see initial-plan.md Phase 6
	if err != nil {
		fmt.Fprintln(os.Stderr, "songarooni: opening log file:", err)
		os.Exit(1)
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// data
	data, err := data.LoadJSON(cfg.DataFile, cfg.MediaDir, cfg.SlideIntervalSeconds)
	if err != nil {
		log.Fatalf("data file invalid, refusing to start:\n%v", err)
	}
	log.Printf("loaded %d songs from %s", len(data.Features), cfg.DataFile)

	// control panel
	controlPanelServer := control_panel.NewControlPanelServer(cfg.ControlPanelPort, cfg.MediaDir)
	controlPanelShutdown, err := controlPanelServer.Run(nil)
	if err != nil {
		log.Fatal("error starting control-panel server", err)
	}

	// shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("shutting down")
	// _ = controller.Stop() // stop makes sure the click dies with the process, not just HTTP
	err = controlPanelShutdown(ctx)
	if err != nil {
		log.Fatal("error shutting down control panel server", err)
	}
	cancel()
}

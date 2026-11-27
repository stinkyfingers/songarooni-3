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
	"songarooni/control_panel"
	"songarooni/data"
	"songarooni/pedal"
	"songarooni/show"
	"songarooni/state"
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
	featureData, err := data.LoadJSON(cfg.DataFile, cfg.MediaDir, cfg.SlideIntervalSeconds)
	if err != nil {
		log.Fatalf("data file invalid, refusing to start:\n%v", err)
	}
	log.Printf("loaded %d songs from %s", len(featureData), cfg.DataFile)

	// show
	showRunner := show.New(cfg.ShowPort, cfg.MediaDir, featureData, cfg.SlideIntervalSeconds, cfg.ControlPanelPort)
	showShutdown, err := showRunner.Run(ctx)
	if err != nil {
		log.Fatal("error running show", err)
	}
	log.Printf("running show server on port %d\n", cfg.ShowPort)

	// controller
	controller := state.NewController(featureData, showRunner)

	// control panel
	controlPanelServer := control_panel.NewControlPanelServer(cfg.ControlPanelPort, cfg.MediaDir)
	controlPanelShutdown, err := controlPanelServer.Run(controller)
	if err != nil {
		log.Fatal("error starting control-panel server", err)
	}
	log.Printf("running control panel server on port %d\n", cfg.ControlPanelPort)

	// kiosk
	kiosk := show.NewKiosk(cfg.ShowPort, cfg.BrowserCommand, cfg.NoKiosk)
	kioskShutdown, err := kiosk.Run(ctx)
	if err != nil {
		log.Fatal("error running kiosk", err)
	}
	log.Printf("running kiosk on port %d\n", cfg.ShowPort)
	defer kioskShutdown() // shutdown no matter what

	// pedal
	pedalShutdown := pedal.Watch(ctx, cfg.PedalDeviceName, controller)
	defer pedalShutdown()

	// shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("shutting down")
	err = controlPanelShutdown(ctx)
	if err != nil {
		log.Fatal("error shutting down control panel server", err)
	}
	err = showShutdown(ctx)
	if err != nil {
		log.Fatal("error shutting down show server")
	}

	log.Println("shut down complete")
	cancel()
}

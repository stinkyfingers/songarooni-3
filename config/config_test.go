package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg, defaults()) {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
}

func TestLoadEmptyFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("# just a comment\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg, defaults()) {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("adrr: \":9090\"\n"), 0644); err != nil { // typo: "adrr"
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for an unknown config key")
	}
}

func TestIntervalLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("slide_interval_seconds: 10s\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.SlideIntervalSeconds != 10*time.Second {
		t.Fatalf("expected SlideIntervalSeconds to be 10s, got %v", cfg.SlideIntervalSeconds)
	}
}

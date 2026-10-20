// Command press-start is the service of the home server. It takes every value
// of the home server from environment variables (PS-DEPLOY-003), and records
// as JSON lines on standard output (PS-ALARM-008).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mkuri/press-start/internal/alarm"
	"github.com/mkuri/press-start/internal/core/settings"
	"github.com/mkuri/press-start/internal/platform/alsa"
)

// The environment variables of the service. All are required.
const (
	envSettings     = "PRESS_START_SETTINGS"
	envALSADevice   = "PRESS_START_ALSA_DEVICE"
	envMixerCard    = "PRESS_START_ALSA_MIXER_CARD"
	envMixerControl = "PRESS_START_ALSA_MIXER_CONTROL"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if err := run(ctx, os.Getenv, os.Stdout); err != nil {
		slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("press-start stopped", "error", err)
		stop()
		os.Exit(1)
	}
}

// config holds the values that the environment gives.
type config struct {
	settingsPath string
	alsaDevice   string
	mixerCard    string
	mixerControl string
}

// newPlayerFunc makes the player of the alarm sound from the config.
type newPlayerFunc func(cfg config) alarm.Player

// run reads the environment and runs the scheduler until ctx ends. It returns
// nil when ctx ends.
func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	return runWith(ctx, getenv, stdout, func(cfg config) alarm.Player {
		return &alsa.Player{Device: cfg.alsaDevice, Card: cfg.mixerCard, Control: cfg.mixerControl}
	})
}

func runWith(ctx context.Context, getenv func(string) string, stdout io.Writer, newPlayer newPlayerFunc) error {
	cfg, err := readConfig(getenv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(stdout, nil))
	store, err := settings.NewStore(cfg.settingsPath, logger)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	s := &alarm.Scheduler{
		Clock:    alarm.SystemClock{},
		Settings: store,
		Player:   newPlayer(cfg),
		Logger:   logger,
		Location: time.Local,
	}
	if err := s.Run(ctx); err != nil && !errors.Is(err, ctx.Err()) {
		return err
	}
	return nil
}

// readConfig reads the variables. Its error names each missing variable.
func readConfig(getenv func(string) string) (config, error) {
	var missing []string
	get := func(name string) string {
		v := getenv(name)
		if v == "" {
			missing = append(missing, name)
		}
		return v
	}
	cfg := config{
		settingsPath: get(envSettings),
		alsaDevice:   get(envALSADevice),
		mixerCard:    get(envMixerCard),
		mixerControl: get(envMixerControl),
	}
	if len(missing) > 0 {
		return config{}, fmt.Errorf("missing environment variables: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

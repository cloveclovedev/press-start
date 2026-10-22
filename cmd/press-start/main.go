// Command press-start is the service of the home server. It takes every value
// of the home server from environment variables, and records as JSON lines on
// standard output.
//
// "press-start auth -client <path>" gets the Google refresh token once, on a
// computer with a browser, and prints it to standard output.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	// The zone data is in the binary, so TZ loads without zone data on the
	// host or in the image.
	_ "time/tzdata"

	"github.com/mkuri/press-start/internal/alarm"
	"github.com/mkuri/press-start/internal/core/settings"
	"github.com/mkuri/press-start/internal/platform/alsa"
	"github.com/mkuri/press-start/internal/platform/google"
)

// The environment variables of the service. All are required.
const (
	envSettings     = "PRESS_START_SETTINGS"
	envALSADevice   = "PRESS_START_ALSA_DEVICE"
	envMixerCard    = "PRESS_START_ALSA_MIXER_CARD"
	envMixerControl = "PRESS_START_ALSA_MIXER_CONTROL"
	// envTZ is the time zone of the wake time. The process reads it too, but
	// Go uses UTC without an error when the zone does not load, so run loads
	// it again and stops on an error.
	envTZ = "TZ"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if len(os.Args) > 1 && os.Args[1] == "auth" {
		if err := runAuth(ctx, os.Args[2:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "press-start auth:", err)
			stop()
			os.Exit(1)
		}
		return
	}
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
	location     *time.Location
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
		Location: cfg.location,
	}
	if err := s.Run(ctx); err != nil && !errors.Is(err, ctx.Err()) {
		return err
	}
	return nil
}

// readConfig reads the variables and loads the time zone of TZ. Its error
// names each missing variable, or TZ when the zone does not load.
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
	tz := get(envTZ)
	if len(missing) > 0 {
		return config{}, fmt.Errorf("missing environment variables: %s", strings.Join(missing, ", "))
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return config{}, fmt.Errorf("load time zone %s=%q: %w", envTZ, tz, err)
	}
	cfg.location = loc
	return cfg, nil
}

// runAuth runs the subcommand auth. It prints the consent URL to stderr and
// the refresh token once to stdout. It writes the token to no file: the
// Maintainer stores it in the secret manager.
func runAuth(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return runAuthWith(ctx, args, stdout, stderr, func(u string) {
		fmt.Fprintf(stderr, "Open this URL in a browser and give consent:\n\n%s\n\n", u)
	})
}

func runAuthWith(ctx context.Context, args []string, stdout, stderr io.Writer, showURL func(string)) error {
	fs := flag.NewFlagSet("auth", flag.ContinueOnError)
	fs.SetOutput(stderr)
	client := fs.String("client", "", `path of the OAuth client JSON file (type "Desktop app")`)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *client == "" || fs.NArg() > 0 {
		fs.Usage()
		return errors.New("usage: press-start auth -client <path>")
	}
	cfg, err := google.LoadConfig(*client)
	if err != nil {
		return err
	}
	// Port 0 takes a free port. Google accepts any port on 127.0.0.1 for a
	// client of the type "Desktop app".
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen on 127.0.0.1: %w", err)
	}
	token, err := google.RefreshToken(ctx, cfg, ln, showURL)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, token)
	return err
}

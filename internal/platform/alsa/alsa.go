// Package alsa plays sound on the speaker of the home server through ALSA.
//
// The sound plays with mpv to ALSA directly, never through PipeWire, so that
// it plays with nobody logged in (PS-ALARM-005.2). The volume is set on the
// ALSA mixer with amixer before the sound plays (PS-ALARM-005.6).
package alsa

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// Runner makes the command that the player runs. Tests replace it so that
// they need no speaker.
type Runner interface {
	Command(ctx context.Context, name string, args ...string) *exec.Cmd
}

// ExecRunner runs the commands with os/exec.
type ExecRunner struct{}

// Command returns exec.CommandContext(ctx, name, args...).
func (ExecRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// Player sets the speaker volume and plays sound files.
type Player struct {
	// Device is the ALSA device that mpv plays to, such as "default" or
	// "hw:1,0".
	Device string
	// Card is the ALSA card of the mixer, such as "0".
	Card string
	// Control is the mixer control of the volume, such as "Master".
	Control string
	// Runner makes the commands. A nil Runner uses ExecRunner.
	Runner Runner
}

func (p *Player) runner() Runner {
	if p.Runner == nil {
		return ExecRunner{}
	}
	return p.Runner
}

// SetVolume sets the mixer control to percent, from 0 to 100.
func (p *Player) SetVolume(ctx context.Context, percent int) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("alsa: volume %d is outside 0 to 100", percent)
	}
	cmd := p.runner().Command(ctx, "amixer", "-c", p.Card, "sset", p.Control, strconv.Itoa(percent)+"%")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("alsa: amixer: %w: %s", err, out)
	}
	return nil
}

// Play plays file in a loop for length, then stops mpv (PS-ALARM-005.4).
// The stop at the length is a success. Play returns an error when mpv exits
// before the length, or when ctx ends first.
func (p *Player) Play(ctx context.Context, file string, length time.Duration) error {
	if length <= 0 {
		return fmt.Errorf("alsa: length %v is not positive", length)
	}
	playCtx, cancel := context.WithTimeout(ctx, length)
	defer cancel()
	cmd := p.runner().Command(playCtx, "mpv",
		"--ao=alsa",
		"--audio-device=alsa/"+p.Device,
		"--no-video",
		"--loop-file=inf",
		"--really-quiet",
		file,
	)
	// The pipe of the output stays open while a child of mpv holds it.
	// WaitDelay keeps Play from waiting on it after the stop.
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("alsa: play %s: %w", file, ctx.Err())
	}
	if errors.Is(playCtx.Err(), context.DeadlineExceeded) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("alsa: mpv: %w: %s", err, out)
	}
	return fmt.Errorf("alsa: mpv exited before the length of %v", length)
}

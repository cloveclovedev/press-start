package alsa

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRunner runs the test binary as a helper process in place of the
// command, in the pattern of the os/exec tests. The helper process behaves
// as mode says.
type fakeRunner struct {
	mode string

	mu    sync.Mutex
	names []string
	args  [][]string
	cmds  []*exec.Cmd
}

func (r *fakeRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=TestHelperProcess", "--", name}, args...)...)
	cmd.Env = append(os.Environ(), "PRESS_START_HELPER_MODE="+r.mode)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, name)
	r.args = append(r.args, args)
	r.cmds = append(r.cmds, cmd)
	return cmd
}

// TestHelperProcess is the fake command. It runs only as a helper process.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("PRESS_START_HELPER_MODE")
	switch mode {
	case "":
		return
	case "ok":
		os.Exit(0)
	case "fail":
		fmt.Fprint(os.Stderr, "cannot open the device")
		os.Exit(2)
	case "long":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(3)
}

func TestSetVolume(t *testing.T) {
	r := &fakeRunner{mode: "ok"}
	p := &Player{Device: "default", Card: "1", Control: "Master", Runner: r}
	if err := p.SetVolume(context.Background(), 80); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}
	want := []string{"-c", "1", "sset", "Master", "80%"}
	if len(r.names) != 1 || r.names[0] != "amixer" || !reflect.DeepEqual(r.args[0], want) {
		t.Errorf("commands = %v %v, want amixer %v", r.names, r.args, want)
	}
}

func TestSetVolumeError(t *testing.T) {
	p := &Player{Card: "1", Control: "Master", Runner: &fakeRunner{mode: "fail"}}
	err := p.SetVolume(context.Background(), 80)
	if err == nil || !strings.Contains(err.Error(), "cannot open the device") {
		t.Errorf("SetVolume error = %v, want the output of amixer", err)
	}
}

func TestSetVolumeOutOfRange(t *testing.T) {
	for _, v := range []int{-1, 101} {
		r := &fakeRunner{mode: "ok"}
		p := &Player{Card: "1", Control: "Master", Runner: r}
		if err := p.SetVolume(context.Background(), v); err == nil {
			t.Errorf("SetVolume(%d) = nil, want an error", v)
		}
		if len(r.names) != 0 {
			t.Errorf("SetVolume(%d) ran %v, want no command", v, r.names)
		}
	}
}

func TestPlayArgs(t *testing.T) {
	for _, file := range []string{"testdata/silence.mp3", "testdata/silence.wav"} {
		t.Run(file, func(t *testing.T) {
			r := &fakeRunner{mode: "long"}
			p := &Player{Device: "hw:1,0", Card: "1", Control: "Master", Runner: r}
			if err := p.Play(context.Background(), file, 200*time.Millisecond); err != nil {
				t.Fatalf("Play: %v", err)
			}
			want := []string{"--ao=alsa", "--audio-device=alsa/hw:1,0", "--no-video", "--loop-file=inf", "--really-quiet", file}
			if len(r.names) != 1 || r.names[0] != "mpv" || !reflect.DeepEqual(r.args[0], want) {
				t.Errorf("commands = %v %v, want mpv %v", r.names, r.args, want)
			}
		})
	}
}

func TestPlayStopsAtLength(t *testing.T) {
	r := &fakeRunner{mode: "long"}
	p := &Player{Device: "default", Runner: r}
	start := time.Now()
	if err := p.Play(context.Background(), "testdata/silence.wav", 300*time.Millisecond); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if d := time.Since(start); d < 300*time.Millisecond || d > 10*time.Second {
		t.Errorf("Play returned after %v, want about 300ms", d)
	}
	state := r.cmds[0].ProcessState
	if state == nil || state.Success() {
		t.Errorf("process state = %v, want the process stopped before its end", state)
	}
}

func TestPlayError(t *testing.T) {
	p := &Player{Device: "default", Runner: &fakeRunner{mode: "fail"}}
	err := p.Play(context.Background(), "testdata/silence.mp3", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "cannot open the device") {
		t.Errorf("Play error = %v, want the output of mpv", err)
	}
}

func TestPlayEarlyExit(t *testing.T) {
	p := &Player{Device: "default", Runner: &fakeRunner{mode: "ok"}}
	if err := p.Play(context.Background(), "testdata/silence.mp3", time.Minute); err == nil {
		t.Error("Play = nil, want an error for an exit before the length")
	}
}

func TestPlayCanceled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	p := &Player{Device: "default", Runner: &fakeRunner{mode: "long"}}
	if err := p.Play(ctx, "testdata/silence.mp3", time.Minute); err == nil {
		t.Error("Play = nil, want an error when ctx ends before the length")
	}
}

// nullRunner runs the real command, with the null audio output of mpv in
// place of ALSA.
type nullRunner struct{}

func (nullRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	var out []string
	for _, a := range args {
		switch {
		case a == "--ao=alsa":
			out = append(out, "--ao=null")
		case strings.HasPrefix(a, "--audio-device="):
		default:
			out = append(out, a)
		}
	}
	return exec.CommandContext(ctx, name, out...)
}

func TestFixturePlay(t *testing.T) {
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv is not installed")
	}
	for _, file := range []string{"testdata/silence.mp3", "testdata/silence.wav"} {
		t.Run(file, func(t *testing.T) {
			p := &Player{Device: "default", Runner: nullRunner{}}
			if err := p.Play(context.Background(), file, 2*time.Second); err != nil {
				t.Errorf("Play: %v", err)
			}
		})
	}
}

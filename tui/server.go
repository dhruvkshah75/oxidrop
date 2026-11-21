package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pelletier/go-toml/v2"
)

type settings struct {
	Share   string `toml:"share_dir"`
	Binary  string `toml:"server_binary"`
	Project string `toml:"-"`
}

func loadSettings(home string) (settings, error) {
	conf := filepath.Join(home, ".config", "oxidrop", "config.toml")
	cfg := settings{}
	data, err := os.ReadFile(conf)
	if err == nil {
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("%s: %w", conf, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}

	resolve := func(path string) string {
		switch {
		case path == "~":
			return home
		case len(path) >= 2 && path[:2] == "~/":
			return filepath.Join(home, path[2:])
		case filepath.IsAbs(path):
			return filepath.Clean(path)
		default:
			return filepath.Join(filepath.Dir(conf), path)
		}
	}
	if cfg.Share == "" {
		cfg.Share = filepath.Join(home, "oxidrop-share")
	} else {
		cfg.Share = resolve(cfg.Share)
	}
	if err := os.MkdirAll(cfg.Share, 0700); err != nil {
		return cfg, err
	}
	cfg.Share, err = filepath.EvalSymlinks(cfg.Share)
	if err != nil {
		return cfg, err
	}
	cwd, _ := os.Getwd()
	exe, _ := os.Executable()
	var roots []string
	if cfg.Binary != "" {
		cfg.Binary = resolve(cfg.Binary)
		roots = append(roots, filepath.Dir(cfg.Binary))
	}
	for _, start := range append(roots, cwd, filepath.Dir(exe)) {
		if cfg.Project = findProject(start); cfg.Project != "" {
			break
		}
	}
	switch {
	case cfg.Binary != "":
	case cfg.Project != "":
		cfg.Binary = filepath.Join(cfg.Project, "target", "release", "oxidrop")
	default:
		sibling := filepath.Join(filepath.Dir(exe), "oxidrop")
		if info, err := os.Stat(sibling); err == nil && info.Mode().IsRegular() {
			cfg.Binary = sibling
		} else if binary, err := exec.LookPath("oxidrop"); err == nil {
			cfg.Binary, err = filepath.Abs(binary)
			if err != nil {
				return cfg, err
			}
		}
	}
	return cfg, nil
}

func findProject(path string) string {
	for filepath.IsAbs(path) {
		manifest, err := os.ReadFile(filepath.Join(path, "Cargo.toml"))
		if err == nil {
			var project struct {
				Package struct{ Name string }
			}
			if toml.Unmarshal(manifest, &project) == nil && project.Package.Name == "oxidrop" {
				return path
			}
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return ""
}

type serverEvent struct {
	Event    string `json:"event"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	Message  string `json:"message"`
}

type serverStatus string
type serverLog string
type sudoNeeded struct{}
type sudoResult struct{ err error }
type serverExit struct{ err error }

func publish(ctx context.Context, events chan<- tea.Msg, msg tea.Msg) {
	select {
	case events <- msg:
	case <-ctx.Done():
	}
}

func runServer(ctx context.Context, cfg settings, elevated bool, events chan<- tea.Msg) {
	if cfg.Binary == "" {
		publish(ctx, events, serverExit{errors.New("set server_binary in config.toml or launch from the oxidrop repository")})
		return
	}
	if _, err := os.Stat(cfg.Binary); errors.Is(err, os.ErrNotExist) && cfg.Project != "" {
		publish(ctx, events, serverStatus("BUILDING"))
		if err := runCommand(ctx, cfg.Project, []string{"cargo", "build", "--release", "--locked", "--target-dir", filepath.Join(cfg.Project, "target")}, func(line string) {
			publish(ctx, events, serverLog(line))
		}); err != nil {
			publish(ctx, events, serverExit{fmt.Errorf("cargo build: %w", err)})
			return
		}
	}

	publish(ctx, events, serverStatus("STARTING"))
	socket, closeSocket, err := supervisorSocket(ctx)
	if err != nil {
		publish(ctx, events, serverExit{fmt.Errorf("open supervisor socket: %w", err)})
		return
	}
	defer closeSocket()
	args := []string{cfg.Binary, "--share-dir", cfg.Share, "--managed-smb", "--quiet", "--supervisor-socket", socket}
	if elevated {
		args = append([]string{"sudo", "-n", "--"}, args...)
	}
	err = runCommand(ctx, "", args, func(line string) {
		var event serverEvent
		if json.Unmarshal([]byte(line), &event) == nil && (event.Event == "ready" || event.Event == "error" || event.Event == "privilege_required") {
			publish(ctx, events, event)
		} else {
			publish(ctx, events, serverLog(line))
		}
	})
	var exit *exec.ExitError
	if !elevated && errors.As(err, &exit) && exit.ExitCode() == 77 {
		publish(ctx, events, sudoNeeded{})
		return
	}
	publish(ctx, events, serverExit{err})
}

// The socket also lets an elevated backend observe the parent's exit.
func supervisorSocket(parent context.Context) (string, func(), error) {
	dir, err := os.MkdirTemp("/tmp", "oxidrop-tui-")
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, "supervisor")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		os.Remove(dir)
		return "", nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	context.AfterFunc(ctx, func() { listener.Close() })
	go func() {
		defer close(done)
		if conn, err := listener.AcceptUnix(); err == nil {
			<-ctx.Done()
			conn.Close()
		}
	}()
	return path, func() {
		cancel()
		<-done
		listener.Close()
		os.Remove(dir)
	}, nil
}

// Assigning the same writer to stdout and stderr serializes calls to Write.
type lineWriter struct {
	buffer bytes.Buffer
	line   func(string)
}

func (w *lineWriter) Write(data []byte) (int, error) {
	for rest := data; len(rest) > 0; {
		before, after, newline := bytes.Cut(rest, []byte{'\n'})
		const maxLine = 65536
		w.buffer.Write(before[:min(len(before), maxLine-w.buffer.Len())])
		if !newline {
			break
		}
		w.flush()
		rest = after
	}
	return len(data), nil
}

func (w *lineWriter) flush() {
	if w.buffer.Len() != 0 {
		w.line(w.buffer.String())
		w.buffer.Reset()
	}
}

func runCommand(ctx context.Context, dir string, args []string, line func(string)) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	w := &lineWriter{line: line}
	cmd.Stdout, cmd.Stderr = w, w
	cmd.WaitDelay = 9 * time.Second
	var force *time.Timer
	cmd.Cancel = func() error {
		pid := cmd.Process.Pid
		force = time.AfterFunc(8*time.Second, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
		return syscall.Kill(-pid, syscall.SIGTERM)
	}
	err := cmd.Run()
	if force != nil {
		force.Stop()
	}
	w.flush()
	return err
}

func lanIP() string {
	if conn, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).IP.String()
	}
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		if ip, ok := addr.(*net.IPNet); ok && ip.IP.To4() != nil && ip.IP.IsGlobalUnicast() {
			return ip.IP.String()
		}
	}
	return "Unavailable"
}

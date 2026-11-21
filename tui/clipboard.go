package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sys/unix"
)

var imageTypes = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp",
	"image/gif": ".gif", "image/bmp": ".bmp", "image/tiff": ".tiff",
}

func pastedPaths(text string) []string {
	text = strings.TrimSpace(text)
	if filepath.IsAbs(text) {
		if _, err := os.Lstat(text); err == nil {
			return []string{text}
		}
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 1 && !strings.HasPrefix(text, "file://") {
		lines = dropWords(text)
	}
	var paths []string
	for _, line := range lines {
		path := strings.TrimSpace(line)
		if path == "" || strings.HasPrefix(path, "#") || path == "copy" || path == "cut" {
			continue
		}
		if strings.HasPrefix(path, "file://") {
			uri, err := url.Parse(path)
			if err != nil || (uri.Host != "" && uri.Host != "localhost") {
				return nil
			}
			path = uri.Path
		}
		if !filepath.IsAbs(path) {
			return nil
		}
		paths = append(paths, filepath.Clean(path))
	}
	return paths
}

// Terminals may paste dropped paths with shell quoting. Parse the quoting as data.
func dropWords(text string) []string {
	var words []string
	var word strings.Builder
	var quote rune
	escaped := false
	for _, ch := range text {
		switch {
		case escaped:
			word.WriteRune(ch)
			escaped = false
		case ch == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				word.WriteRune(ch)
			}
		case ch == '\'' || ch == '"':
			quote = ch
		case ch == ' ' || ch == '\t':
			if word.Len() > 0 {
				words = append(words, word.String())
				word.Reset()
			}
		default:
			word.WriteRune(ch)
		}
	}
	if quote != 0 || escaped {
		return nil
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}
	return words
}

func clipboardCommand(ctx context.Context, mime string) *exec.Cmd {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", mime)
	}
	return exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-o", "-t", mime)
}

func readClipboard(ctx context.Context) (string, string, error) {
	command := exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-o", "-t", "TARGETS")
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		command = exec.CommandContext(ctx, "wl-paste", "--list-types")
	}
	list, err := command.Output()
	if err != nil {
		return "", "", fmt.Errorf("could not read the desktop clipboard (requires wl-paste or xclip): %w", err)
	}
	types := strings.Fields(string(list))
	for _, preferred := range []string{"text/uri-list", "x-special/gnome-copied-files", "image/png", "image/jpeg", "image/webp", "image/gif", "image/bmp", "image/tiff", "UTF8_STRING", "text/plain;charset=utf-8", "text/plain"} {
		for _, mime := range types {
			if preferred != mime {
				continue
			}
			if imageTypes[mime] != "" {
				return mime, "", nil
			}
			if data, err := clipboardCommand(ctx, mime).Output(); err == nil && len(pastedPaths(string(data))) != 0 {
				return "", string(data), nil
			}
		}
	}
	return "", "", errors.New("the clipboard has no supported image or local file paths")
}

func saveClipboardImage(ctx context.Context, folder, share, mime string) error {
	root, err := openPane(folder, share)
	if err != nil {
		return err
	}
	defer root.Close()
	token := rand.Text()
	temp := ".oxidrop-transfer-" + token
	name := "Clipboard-" + time.Now().Format("20060102-150405") + "-" + token[:6] + imageTypes[mime]
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	command := clipboardCommand(ctx, mime)
	command.Stdout = file
	if err := errors.Join(command.Run(), file.Close()); err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return rename(dir, temp, dir, name, unix.RENAME_NOREPLACE)
}

func (m *model) paste(text string, desktop bool) tea.Cmd {
	if m.busy {
		return nil
	}
	wasSpinning := m.spinning()
	m.busy, m.message = true, ""
	m.progress = transferProgress{action: "Paste", name: "Clipboard", index: 1, total: 1}
	to, share := m.panes[m.active].path, m.cfg.Share
	m.tasks.Go(func() {
		if desktop {
			ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
			defer cancel()
			mime, value, err := readClipboard(ctx)
			if err != nil {
				publish(m.ctx, m.events, jobDone{errors: []error{err}})
				return
			}
			if mime != "" {
				if err := saveClipboardImage(ctx, to, share, mime); err != nil {
					publish(m.ctx, m.events, jobDone{errors: []error{err}})
				} else {
					publish(m.ctx, m.events, jobDone{completed: 1})
				}
				return
			}
			text = value
		}
		paths := pastedPaths(text)
		if len(paths) == 0 {
			publish(m.ctx, m.events, jobDone{errors: []error{errors.New("drop local files or use Ctrl+V for a clipboard image")}})
			return
		}
		runJob(m.ctx, job{action: "Paste", to: to, share: share, names: paths}, m.events)
	})
	if !wasSpinning {
		return m.spinner.Tick
	}
	return nil
}

package main

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/unix"
)

func TestTransfer(t *testing.T) {
	ctx := context.Background()
	src, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()

	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	read := func(root *os.Root, path, expect string) {
		t.Helper()
		got, err := root.ReadFile(path)
		if err != nil || string(got) != expect {
			t.Fatalf("%s: got %q, %v; want %q", path, got, err, expect)
		}
	}
	nocollision := func(target string) decision {
		t.Fatalf("unexpected collision: %s", target)
		return decision{}
	}
	check(src.MkdirAll("photos/day", 0750))
	check(src.WriteFile("photos/day/a.jpg", []byte("photo"), 0600))
	check(transfer(ctx, src, dst, "photos", false, nocollision))
	read(dst, "photos/day/a.jpg", "photo")

	check(dst.WriteFile("report", []byte("previous"), 0600))
	check(src.WriteFile("report", []byte("new"), 0600))
	skipped := transfer(ctx, src, dst, "report", true, func(string) decision {
		return decision{action: "skip"}
	})
	if !errors.Is(skipped, errSkipped) {
		t.Fatalf("skip: %v", skipped)
	}
	read(src, "report", "new")
	read(dst, "report", "previous")

	check(transfer(ctx, src, dst, "report", false, func(string) decision {
		return decision{action: "rename", name: "report-copy"}
	}))
	read(dst, "report-copy", "new")
	check(transfer(ctx, src, dst, "report", true, func(string) decision {
		return decision{action: "overwrite"}
	}))
	read(dst, "report", "new")
	if _, err := src.Stat("report"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected the source to be removed: %v", err)
	}

	check(src.Mkdir("broken", 0700))
	check(unix.Mkfifo(filepath.Join(src.Name(), "broken", "pipe"), 0600))
	check(dst.WriteFile("broken", []byte("keep me"), 0600))
	if err := transfer(ctx, src, dst, "broken", false, func(string) decision {
		return decision{action: "overwrite"}
	}); err == nil {
		t.Fatal("expected the unsupported file to abort the copy")
	}
	read(dst, "broken", "keep me")
	entries, err := fs.ReadDir(dst.FS(), ".")
	check(err)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".oxidrop-transfer-") {
			t.Fatalf("leftover staging directory: %s", entry.Name())
		}
	}
}

func TestShareWatchAndView(t *testing.T) {
	share := t.TempDir()
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := &model{
		ctx: ctx, events: make(chan tea.Msg, 4), watcher: watcher,
		cfg: settings{Share: share}, status: "ONLINE", width: 100, height: 24,
		logView: viewport.New(1, 1),
	}
	m.panes[0].open(t.TempDir())
	m.panes[1].open(share)
	m.watchShare()
	go m.watchEvents()
	if err := os.WriteFile(filepath.Join(share, "iPad upload.png"), []byte("picture"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-m.events:
		if _, ok := msg.(shareChanged); !ok {
			t.Fatalf("unexpected watcher message: %T", msg)
		}
		m.Update(msg)
	case <-time.After(3 * time.Second):
		t.Fatal("right pane did not receive a filesystem event")
	}
	if m.panes[1].current() != "iPad upload.png" {
		t.Fatalf("right pane did not show the upload: %q", m.panes[1].current())
	}

	for _, width := range []int{64, 100} {
		m.width = width
		m.showLogs = false
		m.showHelp = false
		view := m.View()
		if !strings.Contains(view, "LINUX HOST") || !strings.Contains(view, "IPAD SHARED FOLDER") {
			t.Fatal(view)
		}
		checkDimensions(t, m, view)
		for index, box := range m.paneBoxes() {
			lines := strings.Split(ansi.Strip(view), "\n")
			if !strings.HasPrefix(lines[box.y], "  ╭") || !strings.HasSuffix(lines[box.y], "╮  ") {
				t.Fatalf("pane row lacks equal margins: %q", lines[box.y])
			}
			if pane, _ := m.mouseHit(box.x, box.y); pane != index {
				t.Fatalf("pane %d border hit pane %d", index, pane)
			}
			if pane, _ := m.mouseHit(width-1, box.y); pane != -1 {
				t.Fatalf("right margin hit pane %d", pane)
			}
		}
		m.showHelp = true
		view = m.View()
		if !strings.Contains(view, "oxidrop / Help") {
			t.Fatal(view)
		}
		checkDimensions(t, m, view)

		m.showLogs = true
		m.ready = serverEvent{Username: "example"}
		m.addLog("server started")
		m.resizeLog()
		view = m.View()
		if !strings.Contains(view, "Use your existing Samba password") || !strings.Contains(view, "server started") {
			t.Fatal(view)
		}
		checkDimensions(t, m, view)
	}
}

func checkDimensions(t *testing.T, m *model, view string) {
	t.Helper()
	if width, height := lipgloss.Size(view); width != m.width || height != m.height {
		t.Fatalf("view uses %d×%d, expected %d×%d\n%s", width, height, m.width, m.height, view)
	}
}

func TestSupervisorDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path, cleanup, err := supervisorSocket(ctx)
	if errors.Is(err, unix.EPERM) {
		t.Skip("Unix sockets are unavailable in this sandbox")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cancel()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if n, err := conn.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatalf("expected parent disconnect, got %d bytes and %v", n, err)
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("parent did not disconnect")
	}
}

func TestRecursiveSearchAndNavigation(t *testing.T) {
	folder := t.TempDir()
	nested := filepath.Join(folder, "album", "weekend")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "queue.png"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "quick.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var tasks sync.WaitGroup
	defer func() { cancel(); tasks.Wait() }()
	m := &model{ctx: ctx, tasks: &tasks, events: make(chan tea.Msg, 16), input: textinput.New()}
	m.panes[0].open(folder)
	m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.panes[0].filter != "q" || m.panes[0].current() != "quick.txt" {
		t.Fatal("typing q should immediately filter the current folder")
	}
	for m.panes[0].searchContext != nil {
		select {
		case msg := <-m.events:
			m.Update(msg)
		case <-time.After(3 * time.Second):
			t.Fatal("recursive search did not finish")
		}
	}
	m.key(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.panes[0].current(); got != filepath.Join("album", "weekend", "queue.png") {
		t.Fatalf("search selected %q", got)
	}
	m.key(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if m.panes[0].path != nested || m.panes[0].current() != "queue.png" {
		t.Fatal("Alt+Enter should reveal a matching file in its directory")
	}
	m.key(tea.KeyMsg{Type: tea.KeyLeft})
	if m.active != 0 || m.panes[0].path != filepath.Dir(nested) {
		t.Fatal("Left should navigate up in the active pane")
	}
	m.key(tea.KeyMsg{Type: tea.KeyTab})
	if m.active != 1 {
		t.Fatal("Tab should switch panes")
	}
}

func TestClipboardImport(t *testing.T) {
	folder := t.TempDir()
	share := t.TempDir()
	mock := filepath.Join(folder, "wl-paste")
	if err := os.WriteFile(mock, []byte("#!/bin/sh\nif [ \"$1\" = --list-types ]; then echo image/png; else printf 'test image'; fi\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", folder)
	t.Setenv("WAYLAND_DISPLAY", "test")
	mime, _, err := readClipboard(context.Background())
	if err != nil || mime != "image/png" {
		t.Fatalf("clipboard MIME = %q, %v", mime, err)
	}
	if err := saveClipboardImage(context.Background(), share, share, mime); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(share)
	if err != nil || len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".png" {
		t.Fatalf("unexpected pasted files: %v, %v", entries, err)
	}
	if data, err := os.ReadFile(filepath.Join(share, entries[0].Name())); err != nil || string(data) != "test image" {
		t.Fatalf("clipboard file = %q, %v", data, err)
	}
	paths := pastedPaths("'" + folder + "/one photo.png' " + folder + "/two\\ photos.png")
	if len(paths) != 2 || filepath.Base(paths[0]) != "one photo.png" || filepath.Base(paths[1]) != "two photos.png" {
		t.Fatalf("drop paths = %v", paths)
	}
}

func TestDragCopiesToOppositeFolder(t *testing.T) {
	host, share := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(host, "photos"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(host, "photos", "photo.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(share, "album"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var tasks sync.WaitGroup
	defer func() { cancel(); tasks.Wait() }()
	m := &model{ctx: ctx, tasks: &tasks, events: make(chan tea.Msg, 8), cfg: settings{Share: share}, width: 100, height: 25}
	m.panes[0].open(host)
	m.panes[0].filter, m.panes[0].searchField = "photo.txt", true
	root, err := os.OpenRoot(host)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	walkMatches(ctx, root.FS(), "", "photo.txt", func(entries []os.DirEntry) { m.panes[0].matches = append(m.panes[0].matches, entries...) })
	m.panes[0].applyFilter("")
	m.panes[1].open(share)
	boxes := m.paneBoxes()
	waitCopy := func() {
		t.Helper()
		tasks.Wait()
		for len(m.events) > 0 {
			if result, ok := (<-m.events).(jobDone); ok {
				if result.completed == 0 || len(result.errors) != 0 {
					t.Fatalf("copy result: %+v", result)
				}
				m.Update(result)
				return
			}
		}
		t.Fatal("no copy completed")
	}
	m.mouse(tea.MouseMsg{X: boxes[0].x + 2, Y: boxes[0].y + 4, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.mouse(tea.MouseMsg{X: boxes[1].x + 2, Y: boxes[1].y + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m.mouse(tea.MouseMsg{X: boxes[1].x + 2, Y: boxes[1].y + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	waitCopy()
	data, err := os.ReadFile(filepath.Join(share, "album", "photo.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("copied file: %q, %v", data, err)
	}

	m.search(0, "")
	m.panes[1].open(filepath.Join(share, "album"))
	if err := os.WriteFile(filepath.Join(share, "album", "another.txt"), []byte("iPad"), 0600); err != nil {
		t.Fatal(err)
	}
	m.panes[1].load()
	m.mouse(tea.MouseMsg{X: boxes[1].x + 2, Y: boxes[1].y + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.mouse(tea.MouseMsg{X: boxes[1].x + 2, Y: boxes[1].y + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if m.active != 1 || m.panes[1].current() != "another.txt" || m.busy {
		t.Fatal("click should focus the right pane and choose the row without copying")
	}
	m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}, Alt: true})
	m.mouse(tea.MouseMsg{X: boxes[1].x + 2, Y: boxes[1].y + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.mouse(tea.MouseMsg{X: boxes[0].x + 2, Y: boxes[0].y + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m.mouse(tea.MouseMsg{X: boxes[0].x + 2, Y: boxes[0].y + 1, Action: tea.MouseActionRelease})
	waitCopy()
	for name, want := range map[string]string{"photo.txt": "hello", "another.txt": "iPad"} {
		if data, err := os.ReadFile(filepath.Join(host, name)); err != nil || string(data) != want {
			t.Fatalf("copied %s from the right pane: %q, %v", name, data, err)
		}
	}

	// Copy from the right, then choose a different destination in the left pane.
	if err := os.Mkdir(filepath.Join(host, "destination"), 0700); err != nil {
		t.Fatal(err)
	}
	m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}, Alt: true})
	m.key(tea.KeyMsg{Type: tea.KeyTab})
	m.navigate(filepath.Join(host, "destination"))
	m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	waitCopy()
	if data, err := os.ReadFile(filepath.Join(host, "destination", "another.txt")); err != nil || string(data) != "iPad" {
		t.Fatalf("pasted file: %q, %v", data, err)
	}
}

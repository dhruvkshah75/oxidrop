package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

type shareChanged struct{}
type ipUpdate string
type startBackend struct{}

type model struct {
	ctx        context.Context
	tasks      *sync.WaitGroup
	events     chan tea.Msg
	watcher    *fsnotify.Watcher
	cfg        settings
	home, ip   string
	panes      [2]filePane
	active     int
	width      int
	height     int
	status     string
	ready      serverEvent
	logs       []string
	logView    viewport.Model
	showLogs   bool
	showHelp   bool
	spinner    spinner.Model
	input      textinput.Model
	prompt     string
	drag       *paneDrag
	helpOffset int
	pending    *collision
	confirm    *job
	progress   transferProgress
	busy       bool
	message    string
}

func main() {
	home, err := os.UserHomeDir()
	if err == nil {
		err = run(home)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "oxidrop-tui:", err)
		os.Exit(1)
	}
}

func run(home string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := loadSettings(home)
	if err != nil {
		return err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	var tasks sync.WaitGroup
	defer func() {
		cancel()
		tasks.Wait()
	}()

	ip := lanIP()
	m := &model{
		ctx: ctx, tasks: &tasks, events: make(chan tea.Msg, 128), watcher: watcher,
		cfg: cfg, home: home, ip: ip, status: "STARTING",
		logView: viewport.New(1, 1), spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		input: textinput.New(),
	}
	m.spinner.Style = accentStyle
	m.panes[0].open(cwd)
	m.panes[1].open(cfg.Share)
	m.watchShare()
	tasks.Go(m.watchEvents)
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx)).Run(); !errors.Is(err, tea.ErrProgramKilled) {
		return err
	}
	return nil
}

func (m *model) watchShare() {
	wanted := map[string]bool{m.cfg.Share: true}
	for path := m.panes[1].path; !wanted[path]; path = filepath.Dir(path) {
		wanted[path] = true
	}
	for _, watched := range m.watcher.WatchList() {
		if !wanted[watched] {
			_ = m.watcher.Remove(watched)
		}
		delete(wanted, watched)
	}
	for dir := range wanted {
		if err := m.watcher.Add(dir); err != nil {
			m.message = "Watch: " + err.Error()
		}
	}
}

func (m *model) watchEvents() {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var refresh <-chan time.Time
	scheduleRefresh := func() {
		if refresh == nil {
			timer.Reset(90 * time.Millisecond)
			refresh = timer.C
		}
	}
	for {
		select {
		case <-m.ctx.Done():
			return
		case _, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			scheduleRefresh()
		case err, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
			publish(m.ctx, m.events, serverLog("Watch: "+err.Error()))
			scheduleRefresh()
		case <-refresh:
			refresh = nil
			publish(m.ctx, m.events, shareChanged{})
		}
	}
}

func (m *model) nextEvent() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-m.events:
			return msg
		case <-m.ctx.Done():
			return nil
		}
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		tea.SetWindowTitle("oxidrop"),
		m.nextEvent(),
		m.spinner.Tick,
		nextIP(),
		tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return startBackend{} }),
	)
}

func nextIP() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return ipUpdate(lanIP()) })
}

func (m *model) spinning() bool {
	return m.busy || (m.status != "ONLINE" && m.status != "OFFLINE")
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case startBackend:
		m.tasks.Go(func() { runServer(m.ctx, m.cfg, false, m.events) })
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeLog()
	case spinner.TickMsg:
		if m.spinning() {
			m.spinner, cmd = m.spinner.Update(msg)
		}
	case ipUpdate:
		m.ip, cmd = string(msg), nextIP()
	case searchResult:
		p := &m.panes[msg.pane]
		if p.searchContext == msg.ctx {
			keep := p.current()
			if msg.entries == nil {
				p.searchContext, p.searchNote = nil, msg.note
			} else {
				p.matches = append(p.matches, msg.entries...)
			}
			p.applyFilter(keep)
		}
		cmd = m.nextEvent()
	case tea.MouseMsg:
		return m, m.mouse(msg)
	case openError:
		m.message = "Could not open file · " + msg.err.Error()
	case tea.KeyMsg:
		return m, m.key(msg)
	case serverStatus:
		m.status = string(msg)
		cmd = m.nextEvent()
	case serverEvent:
		if msg.Event == "ready" {
			m.status, m.ready = "ONLINE", msg
			m.resizeLog()
			m.addLog("SMB listening at " + msg.URL)
		} else {
			m.message = msg.Message
			m.addLog(msg.Message)
		}
		cmd = m.nextEvent()
	case serverLog:
		m.addLog(string(msg))
		cmd = m.nextEvent()
	case sudoNeeded:
		m.addLog("Requesting sudo authorization for managed Samba")
		m.status = "AUTHORIZING"
		m.message = ""
		cmd = tea.Batch(m.nextEvent(), tea.ExecProcess(exec.Command("sudo", "-v"), func(err error) tea.Msg { return sudoResult{err} }))
	case sudoResult:
		if msg.err != nil {
			m.status, m.message = "OFFLINE", "Samba authorization failed: "+msg.err.Error()
			m.addLog(m.message)
		} else {
			m.tasks.Go(func() { runServer(m.ctx, m.cfg, true, m.events) })
		}
	case serverExit:
		m.status, m.ready = "OFFLINE", serverEvent{}
		m.resizeLog()
		if msg.err != nil {
			if m.message == "" {
				m.message = msg.err.Error() + " · Alt+L opens logs"
			}
			m.addLog(msg.err.Error())
		}
		cmd = m.nextEvent()
	case shareChanged:
		m.panes[1].load()
		if os.IsNotExist(m.panes[1].err) && m.panes[1].path != m.cfg.Share {
			m.panes[1].open(m.cfg.Share)
			m.watchShare()
		}
		m.refreshSearch(1)
		cmd = m.nextEvent()
	case transferProgress:
		m.progress = msg
		cmd = m.nextEvent()
	case collision:
		m.pending = &msg
		cmd = m.nextEvent()
	case jobDone:
		m.busy = false
		for i := range m.panes {
			m.panes[i].load()
		}
		m.message = fmt.Sprintf("%s finished · %d completed · %d skipped", m.progress.action, msg.completed, msg.skipped)
		if len(msg.errors) != 0 {
			reason := "see details"
			if errors.Is(msg.errors[0], os.ErrPermission) {
				reason = "access denied"
			}
			m.message = fmt.Sprintf("%s · %d failed (%s) · Alt+L for details", m.message, len(msg.errors), reason)
			for _, err := range msg.errors {
				m.addLog(err.Error())
			}
		}
		m.refreshSearch(0)
		m.refreshSearch(1)
		cmd = m.nextEvent()
	default:
		if m.prompt != "" {
			m.input, cmd = m.input.Update(msg)
		}
	}
	return m, cmd
}

func (m *model) key(key tea.KeyMsg) tea.Cmd {
	k := key.String()
	if k == "ctrl+c" || k == "alt+q" {
		return tea.Quit
	}
	if !m.showLogs && m.pending == nil && m.confirm == nil && m.prompt == "" {
		if k == "ctrl+v" {
			m.closeInput()
			return m.paste("", true)
		}
		if key.Paste && len(pastedPaths(string(key.Runes))) != 0 {
			m.closeInput()
			return m.paste(string(key.Runes), false)
		}
	}
	p := &m.panes[m.active]

	if m.prompt != "" {
		return m.handleInput(p, key)
	}
	if m.pending != nil {
		switch k {
		case "alt+o":
			m.answer("overwrite", "")
		case "alt+s", "esc":
			m.answer("skip", "")
		case "alt+r":
			name := m.pending.target
			ext := filepath.Ext(name)
			if ext != name {
				name = name[:len(name)-len(ext)] + " (copy)" + ext
			} else {
				name += " (copy)"
			}
			return m.edit("rename", "Rename to  ", name)
		}
		return nil
	}
	if m.confirm != nil {
		switch k {
		case "alt+y":
			request := *m.confirm
			m.confirm = nil
			return m.begin(request)
		case "alt+n", "esc":
			m.confirm = nil
		}
		return nil
	}
	if k == "alt+l" || (m.showLogs && k == "esc") {
		m.showLogs = !m.showLogs
		if m.showLogs {
			m.resizeLog()
			m.logView.GotoBottom()
		}
		return nil
	}
	if m.showLogs {
		var cmd tea.Cmd
		m.logView, cmd = m.logView.Update(key)
		return cmd
	}

	if m.showHelp {
		switch k {
		case "f1", "alt+h", "esc":
			m.showHelp = false
		case "up":
			m.helpOffset = max(0, m.helpOffset-1)
		case "down":
			m.helpOffset++
		case "pgup":
			m.helpOffset = max(0, m.helpOffset-max(1, m.height-6))
		case "pgdown":
			m.helpOffset += max(1, m.height-6)
		}
		return nil
	}
	switch k {
	case "f1", "alt+h":
		m.showHelp, m.helpOffset = true, 0
	case "tab", "shift+tab":
		m.active = 1 - m.active
	case "up":
		p.cursor = max(0, p.cursor-1)
	case "down":
		p.cursor = min(max(0, len(p.visible)-1), p.cursor+1)
	case "pgup":
		p.cursor = max(0, p.cursor-max(1, m.height-11))
	case "pgdown":
		p.cursor = min(max(0, len(p.visible)-1), p.cursor+max(1, m.height-11))
	case "enter", "right", "alt+enter":
		if p.current() != "" {
			path := filepath.Join(p.path, p.current())
			if p.visible[p.cursor].IsDir() {
				m.navigate(path)
			} else if k == "alt+enter" {
				m.navigate(filepath.Dir(path))
				if p.path == filepath.Dir(path) {
					m.search(m.active, "")
					p.applyFilter(filepath.Base(path))
				}
			} else {
				if m.active == 1 {
					real, err := filepath.EvalSymlinks(path)
					if err != nil {
						m.message = err.Error()
						return nil
					}
					rel, err := filepath.Rel(m.cfg.Share, real)
					if err != nil || !filepath.IsLocal(rel) {
						m.message = "File is outside the share"
						return nil
					}
					path = real
				}
				return func() tea.Msg {
					if err := exec.Command("xdg-open", path).Run(); err != nil {
						return openError{err}
					}
					return nil
				}
			}
		}
	case "backspace":
		_, size := utf8.DecodeLastRuneInString(p.filter)
		m.search(m.active, p.filter[:len(p.filter)-size])
	case "left":
		if m.active == 0 || p.path != m.cfg.Share {
			m.navigate(filepath.Dir(p.path))
		}
	case "alt+g":
		if m.active == 0 {
			m.navigate(m.home)
		}
	case "alt+u":
		if m.active == 0 {
			m.navigate("/")
		}
	case "alt+/":
		p.searchField = true
	case "alt+r":
		for i := range m.panes {
			m.panes[i].load()
		}
		m.refreshSearch(0)
		m.refreshSearch(1)
	case "esc", "ctrl+u":
		m.message = ""
		m.search(m.active, "")
	case "ctrl+space":
		if name := p.current(); name != "" {
			if p.selected[name] {
				delete(p.selected, name)
			} else {
				p.selected[name] = true
			}
		}
	case "alt+a":
		all := true
		for _, entry := range p.visible {
			all = all && p.selected[entry.Name()]
		}
		for _, entry := range p.visible {
			if all {
				delete(p.selected, entry.Name())
			} else {
				p.selected[entry.Name()] = true
			}
		}
	case "alt+c", "alt+m", "alt+d", "delete", "alt+n", "ctrl+n", "ctrl+f":
		if m.busy {
			return nil
		}
		if k == "alt+n" || k == "ctrl+n" {
			return m.edit("folder", "New folder  ", "")
		}
		if k == "ctrl+f" {
			return m.edit("file", "New file  ", "")
		}
		if k == "delete" {
			k = "alt+d"
		}
		if names := p.targets(); len(names) != 0 {
			action := map[string]string{"alt+c": "Copy", "alt+m": "Move", "alt+d": "Delete"}[k]
			request := job{action, p.path, m.panes[1-m.active].path, m.cfg.Share, names}
			if k == "alt+d" {
				m.confirm = &request
			} else {
				return m.begin(request)
			}
		}
	default:
		if (key.Type == tea.KeyRunes || key.Type == tea.KeySpace) && !key.Alt {
			input := strings.Map(func(r rune) rune {
				if unicode.IsPrint(r) && !unicode.Is(unicode.Cf, r) {
					return r
				}
				return -1
			}, string(key.Runes))
			if key.Type == tea.KeySpace {
				input = " "
			}
			if input != "" {
				m.search(m.active, p.filter+input)
			}
		}
	}
	return nil
}

type openError struct{ err error }

func (m *model) refreshSearch(index int) {
	if query := m.panes[index].filter; query != "" {
		m.search(index, query)
	}
}

func (m *model) navigate(path string) {
	p := &m.panes[m.active]
	real, err := filepath.EvalSymlinks(path)
	if err == nil && m.active == 1 {
		rel, relErr := filepath.Rel(m.cfg.Share, real)
		if relErr != nil || !filepath.IsLocal(rel) {
			err = errors.New("directory is outside the share")
		}
	}
	if err != nil {
		m.message = err.Error()
		return
	}
	if real != p.path {
		p.open(real)
		if m.active == 1 {
			m.watchShare()
		}
	}
}

func (m *model) begin(request job) tea.Cmd {
	wasSpinning := m.spinning()
	m.busy, m.showLogs, m.message = true, false, ""
	m.progress = transferProgress{request.action, request.names[0], 1, len(request.names)}
	m.tasks.Go(func() { runJob(m.ctx, request, m.events) })
	if !wasSpinning {
		return m.spinner.Tick
	}
	return nil
}

func (m *model) answer(action, name string) {
	m.pending.reply <- decision{action, name}
	m.pending = nil
}

func (m *model) edit(kind, prefix, value string) tea.Cmd {
	m.prompt = kind
	m.message = ""
	m.input.Prompt = prefix
	m.input.CharLimit = 0
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.input.Width = max(8, m.width-30)
	return m.input.Focus()
}

func (m *model) closeInput() {
	m.prompt = ""
	m.input.Blur()
}

func (m *model) handleInput(p *filePane, key tea.KeyMsg) tea.Cmd {
	value := m.input.Value()
	switch key.String() {
	case "esc":
		m.closeInput()
		return nil

	case "enter":
		switch m.prompt {
		case "folder", "file", "rename":
			if !validName(value) {
				m.message = "Enter a single folder or file name"
				return nil
			}
			if m.prompt == "rename" {
				m.answer("rename", value)
			} else {
				root, err := openPane(p.path, m.cfg.Share)
				if err == nil {
					if m.prompt == "folder" {
						err = root.Mkdir(value, 0700)
					} else if file, openErr := root.OpenFile(value, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600); openErr == nil {
						err = file.Close()
					} else {
						err = openErr
					}
					root.Close()
				}
				if err != nil {
					m.message = err.Error()
					return nil
				}
				p.load()
				p.applyFilter(value)
				m.message = "Created " + value
			}
		}
		m.closeInput()
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return cmd
}

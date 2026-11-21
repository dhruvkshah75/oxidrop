package main

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	canvas   = lipgloss.Color("#181715")
	soft     = lipgloss.Color("#1f1e1b")
	elevated = lipgloss.Color("#252320")
	ink      = lipgloss.Color("#faf9f5")
	muted    = lipgloss.Color("#a09d96")
	border   = lipgloss.Color("#6c6a64")
	coral    = lipgloss.Color("#cc785c")
	teal     = lipgloss.Color("#5db8a6")
	amber    = lipgloss.Color("#e8a55a")
)

var (
	textStyle   = lipgloss.NewStyle().Foreground(ink)
	mutedStyle  = lipgloss.NewStyle().Foreground(muted)
	accentStyle = lipgloss.NewStyle().Foreground(coral)
)

func display(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return '�'
		}
		return r
	}, s)
}

func row(style lipgloss.Style, width int, s string) string {
	return style.Width(width).Render(ansi.Truncate(s, width, "…"))
}

func (m *model) addLog(line string) {
	atBottom := m.logView.AtBottom()
	m.logs = append(m.logs, display(line))
	const maxLogs = 1000
	if len(m.logs) > maxLogs {
		m.logs = m.logs[len(m.logs)-maxLogs:]
	}
	if m.showLogs {
		m.setLogContent(atBottom)
	}
}

func (m *model) setLogContent(atBottom bool) {
	offset := m.logView.YOffset
	lines := strings.Join(m.logs, "\n")
	if lines == "" {
		lines = "Waiting for server output…"
	}
	m.logView.SetContent(ansi.Hardwrap(lines, max(1, m.logView.Width), true))
	if atBottom {
		m.logView.GotoBottom()
	} else {
		m.logView.SetYOffset(offset)
	}
}

func (m *model) panelHeight() int { return max(1, m.height-4) }

func styledLine(width int, value string, background lipgloss.Color) string {
	value = ansi.Truncate(value, max(0, width), "")
	return value + row(textStyle.Background(background), max(0, width-lipgloss.Width(value)), "")
}

func (m *model) fillScreen(screen string) string {
	lines := strings.Split(screen, "\n")
	lines = lines[:min(len(lines), m.height)]
	for i, line := range lines {
		lines[i] = styledLine(m.width, line, canvas)
	}
	for len(lines) < m.height {
		lines = append(lines, row(textStyle.Background(canvas), m.width, ""))
	}
	return strings.Join(lines, "\n")
}

func (m *model) resizeLog() {
	if !m.showLogs {
		return
	}
	atBottom := m.logView.AtBottom()
	head := 1
	if m.ready.Username != "" {
		head += 2
	}
	m.logView.Width = max(1, m.width-4)
	m.logView.Height = max(1, m.panelHeight()-2-head)
	m.setLogContent(atBottom)
}

func (m *model) View() string {
	width := m.width
	if width <= 0 || m.height <= 0 {
		return ""
	}
	if width < 16 || m.height < 6 {
		return m.fillScreen(row(accentStyle.Background(elevated), width, " oxidrop · Alt+Q"))
	}
	shade, status := teal, "● "+m.status
	switch m.status {
	case "OFFLINE":
		shade = amber
	case "ONLINE":
	default:
		shade, status = amber, ansi.Strip(m.spinner.View())+" "+m.status
	}
	url := m.ready.URL
	if url == "" {
		url = "smb://oxidrop.local"
	}
	base := textStyle.Background(elevated)
	header := styledLine(width, accentStyle.Background(elevated).Bold(true).Render(" oxidrop")+
		base.Foreground(shade).Bold(true).Render("   "+status)+base.Render("   "+url)+
		mutedStyle.Background(elevated).Render("   LAN "+display(m.ip)), elevated)
	boxes := m.paneBoxes()
	var center string
	switch {
	case m.showLogs:
		center = m.renderLogs(width, m.panelHeight())
	case boxes[0].height == 0 || boxes[1].height == 0:
		center = m.renderPane(m.active, boxes[m.active].width, boxes[m.active].height)
	default:
		left := m.renderPane(0, boxes[0].width, boxes[0].height)
		right := m.renderPane(1, boxes[1].width, boxes[1].height)
		if boxes[1].x == boxes[0].x {
			center = lipgloss.JoinVertical(lipgloss.Left, left, row(base.Background(canvas), boxes[0].width, ""), right)
		} else {
			gutter := strings.TrimSuffix(strings.Repeat(row(base.Background(canvas), 2, "")+"\n", boxes[0].height), "\n")
			center = lipgloss.JoinHorizontal(lipgloss.Top, left, gutter, right)
		}
	}
	if !m.showLogs {
		center = lipgloss.PlaceHorizontal(width, lipgloss.Center, center, lipgloss.WithWhitespaceBackground(canvas))
	}
	keys := " [F1] Help   [Tab] Switch   [Alt+C] Copy   [Alt+Q] Quit"
	if width >= 66 {
		keys += "   Type to search"
	}
	if m.showLogs {
		keys = " [↑↓] Scroll   [Alt+L / Esc] Close   [Alt+Q] Quit"
	}
	footer := row(mutedStyle.Background(elevated), width, keys)
	detail := ""
	if m.drag != nil && m.drag.moved {
		if i, hit := m.mouseHit(m.drag.x, m.drag.y); i >= 0 && i != m.drag.from {
			detail = " ↓ Release to copy → " + display(m.dropTarget(i, hit))
		}
	} else if m.busy {
		detail = " " + display(m.progress.name)
	}
	screen := m.fillScreen(strings.Join([]string{header, center, m.renderNotice(width), row(mutedStyle.Background(canvas), width, detail), footer}, "\n"))
	if m.showHelp && !m.showLogs {
		return m.helpDialog(screen)
	}
	if m.prompt != "" || m.pending != nil || m.confirm != nil {
		w := min(74, width-4)
		if m.height >= 7 {
			dialog := frame(w, true).Background(elevated).Render(m.renderNotice(w - 4))
			return centerDialog(screen, dialog, width)
		}
	}
	return screen
}

func frame(width int, active bool) lipgloss.Style {
	color := border
	if active {
		color = coral
	}
	return textStyle.Background(soft).
		Border(lipgloss.RoundedBorder()).BorderForeground(color).BorderBackground(canvas).
		Padding(0, 1).Width(width - 2)
}

func (m *model) renderPane(index, width, height int) string {
	p := &m.panes[index]
	title := "LINUX HOST"
	if index == 1 {
		title = "IPAD SHARED FOLDER"
	}
	targetRow := -1
	targetPane := false
	if m.drag != nil && m.drag.moved && m.drag.from != index {
		if pane, hit := m.mouseHit(m.drag.x, m.drag.y); pane == index {
			title, targetPane = "↓ DROP TO COPY", true
			if hit >= 0 && p.visible[hit].IsDir() {
				targetRow = hit
			}
		}
	}
	active := m.active == index || targetPane
	heading := textStyle.Bold(true)
	if active {
		heading = accentStyle.Bold(true)
	}
	if height < 5 {
		rows := []string{row(heading.Background(soft), width, " "+title)}
		if height > 1 {
			label := display(p.path)
			if p.searchField {
				label = "Search: " + display(p.filter)
			}
			if p.current() != "" {
				label = " › " + display(p.current())
			}
			rows = append(rows, row(textStyle.Background(elevated), width, label))
		}
		for len(rows) < height {
			rows = append(rows, row(textStyle.Background(soft), width, ""))
		}
		return strings.Join(rows, "\n")
	}
	cw := width - 4
	rows := []string{
		row(heading.Background(soft), cw, title),
		row(mutedStyle.Background(soft), cw, display(p.path)),
	}
	first, start, end := p.entryWindow(height)
	if p.searchField {
		label := "Search: " + display(p.filter)
		if p.searchContext != nil {
			label += " · Searching subfolders…"
		}
		if p.searchNote != "" {
			label += " · " + display(p.searchNote)
		}
		search := row(accentStyle.Background(soft), cw, label)
		if first == 4 {
			rows = append(rows, search)
		} else {
			rows[1] = search
		}
	}
	for i := start; i < end; i++ {
		entry := p.visible[i]
		cursor, mark, label := " ", " ", "      "
		style := textStyle.Background(soft)
		if entry.IsDir() {
			style = style.Foreground(teal)
			label = "[DIR] "
		}
		if p.selected[entry.Name()] {
			mark = "✓"
		}
		if i == p.cursor {
			cursor = "›"
			style = style.Background(elevated)
			if m.active == index {
				style = accentStyle.Background(elevated)
			}
		}
		if i == targetRow {
			cursor, style = "↓", accentStyle.Background(elevated)
		}
		if m.drag != nil && m.drag.moved && m.drag.from == index && m.drag.name == entry.Name() {
			cursor, style = "↗", accentStyle.Background(elevated)
		}
		rows = append(rows, row(style, cw, cursor+mark+" "+label+display(entry.Name())))
	}
	if len(rows) < height-2 && (p.err != nil || len(p.visible) == 0) {
		message := "Empty folder"
		if p.err != nil {
			message = display(p.err.Error())
		} else if p.searchContext != nil {
			message = "Searching subfolders…"
		} else if p.filter != "" {
			message = "No matches"
		}
		rows = append(rows, row(mutedStyle.Background(soft), cw, "  "+message))
	}
	for len(rows) < height-2 {
		rows = append(rows, row(textStyle.Background(soft), cw, ""))
	}
	return frame(width, active).Render(strings.Join(rows, "\n"))
}

func centerDialog(screen, dialog string, screenWidth int) string {
	lines, overlay := strings.Split(screen, "\n"), strings.Split(dialog, "\n")
	width := lipgloss.Width(dialog)
	if width > screenWidth || len(overlay) > len(lines) {
		return screen
	}
	x, y := (screenWidth-width)/2, (len(lines)-len(overlay))/2
	for i, line := range overlay {
		behind := lines[y+i]
		lines[y+i] = styledLine(x, ansi.Cut(behind, 0, x), canvas) + line + ansi.Cut(behind, x+width, screenWidth)
	}
	return strings.Join(lines, "\n")
}

func (m *model) helpDialog(screen string) string {
	width := min(60, m.width-2)
	body := width - 4
	lines := []string{
		"Tab                  Switch panes",
		"↑/↓ · mouse wheel    Select a row",
		"Enter/→ · ←          Open · go up",
		"Type                 Search here and below",
		"Backspace · Ctrl+U   Erase · clear search",
		"Alt+Enter            Reveal a matching file",
		"Alt+C · Alt+M        Copy · move across panes",
		"Drag an entry        Copy to opposite pane or folder",
		"Ctrl+V               Paste a file or clipboard image",
		"Ctrl+N · Ctrl+F      Create a folder · empty file",
		"Delete · Alt+D       Delete with confirmation",
		"Ctrl+Space · Alt+A   Select item · select all",
		"Alt+R                Refresh both panes",
		"Alt+L                Server logs and credentials",
		"Alt+Q                Quit and close the SMB session",
	}
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(ansi.Wrap(line, body, ""), "\n")...)
	}
	page := min(14, m.height-4)
	start := max(0, min(m.helpOffset, len(wrapped)-page))
	rows := []string{row(accentStyle.Background(elevated).Bold(true), body, "oxidrop / Help")}
	for _, line := range wrapped[start:min(start+page, len(wrapped))] {
		rows = append(rows, row(textStyle.Background(elevated), body, line))
	}
	rows = append(rows, row(accentStyle.Background(elevated), body, "F1 / Esc close · ↑↓ scroll"))
	return centerDialog(screen, frame(width, true).Background(elevated).Render(strings.Join(rows, "\n")), m.width)
}

func inputRow(input textinput.Model, width int) string {
	base := textStyle.Background(elevated)
	input.PromptStyle = accentStyle.Background(elevated)
	input.TextStyle = base
	input.PlaceholderStyle = mutedStyle.Background(elevated)
	input.Cursor.TextStyle = base
	// Bubbles renders a visible cursor in reverse video.
	input.Cursor.Style = lipgloss.NewStyle().Foreground(coral).Background(elevated)
	return row(base, width, input.View())
}

func (m *model) renderLogs(width, height int) string {
	cw := width - 4
	rows := []string{row(accentStyle.Bold(true).Background(soft), cw, "Server logs")}
	if m.ready.Username != "" {
		rows = append(rows,
			row(textStyle.Background(elevated), cw, "Registered User  "+display(m.ready.Username)),
			row(textStyle.Background(elevated), cw, "Session password "+display(m.ready.Password)),
		)
	}
	rows = append(rows, m.logView.View())
	return frame(width, true).Height(height - 2).Render(strings.Join(rows, "\n"))
}

func (m *model) renderNotice(width int) string {
	style := textStyle.Background(canvas)
	if m.prompt != "" || m.pending != nil || m.confirm != nil {
		style = style.Background(elevated)
	}
	switch {
	case m.prompt != "":
		note := " · Enter confirm · Esc cancel"
		if m.message != "" {
			note = "  " + display(m.message)
		}
		input := m.input
		input.Width = max(4, width-lipgloss.Width(note)-lipgloss.Width(input.Prompt)-2)
		form := inputRow(input, lipgloss.Width(input.Prompt)+input.Width+1)
		return row(style, width, form+style.Render(note))
	case m.pending != nil:
		keys := "Alt+O overwrite  Alt+S skip  Alt+R rename"
		label := ansi.Truncate(display(m.pending.target), max(4, width-len(keys)-20), "…")
		return row(style.Foreground(amber), width, " Already exists "+label+"   "+keys)
	case m.confirm != nil:
		name := display(m.confirm.names[0])
		if len(m.confirm.names) > 1 {
			name = fmt.Sprintf("%d items", len(m.confirm.names))
		}
		return row(style.Foreground(amber), width, " Delete "+ansi.Truncate(name, width-27, "…")+"?   Alt+Y yes   Alt+N no")
	case m.busy:
		p := m.progress
		return row(style, width, fmt.Sprintf(" %s %s %d/%d", ansi.Strip(m.spinner.View()), p.action, p.index, p.total))
	case m.message != "":
		return row(style, width, " "+display(m.message))
	default:
		return row(style, width, "")
	}
}

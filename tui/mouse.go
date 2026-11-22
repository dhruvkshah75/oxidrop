package main

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

type paneBox struct{ x, y, width, height int }

const paneMargin = 2

func (m *model) paneBoxes() [2]paneBox {
	h := m.panelHeight()
	width := m.width - 2*paneMargin
	if m.width >= 66 {
		left := (width - 2) / 2
		return [2]paneBox{{paneMargin, 1, left, h}, {paneMargin + left + 2, 1, width - left - 2, h}}
	}
	if h >= 13 {
		top := (h - 1) / 2
		return [2]paneBox{{paneMargin, 1, width, top}, {paneMargin, top + 2, width, h - top - 1}}
	}
	var boxes [2]paneBox
	boxes[m.active] = paneBox{paneMargin, 1, width, h}
	return boxes
}

func (p *filePane) entryWindow(height int) (first, start, end int) {
	first = 3
	if p.searchField && height >= 6 {
		first++
	}
	capacity := max(0, height-first-1)
	start = max(0, p.cursor-capacity+1)
	return first, start, min(len(p.visible), start+capacity)
}

type paneDrag struct {
	from, x, y   int
	parent, name string
	names        []string
	moved        bool
}

func (m *model) mouseHit(x, y int) (int, int) {
	for i, box := range m.paneBoxes() {
		if x < box.x || x >= box.x+box.width || y < box.y || y >= box.y+box.height {
			continue
		}
		first, start, end := m.panes[i].entryWindow(box.height)
		entry := start + y - box.y - first
		if entry >= start && entry < end && x >= box.x+2 && x < box.x+box.width-2 {
			return i, entry
		}
		return i, -1
	}
	return -1, -1
}

func (m *model) dropTarget(index, row int) string {
	p := &m.panes[index]
	if row >= 0 && p.visible[row].IsDir() {
		return filepath.Join(p.path, p.visible[row].Name())
	}
	return p.path
}

func (m *model) mouse(msg tea.MouseMsg) tea.Cmd {
	if m.showHelp || m.showLogs || m.busy || m.pending != nil || m.confirm != nil || m.prompt != "" || m.width < 16 || m.height < 6 {
		m.drag = nil
		return nil
	}
	index, hit := m.mouseHit(msg.X, msg.Y)
	switch msg.Action {
	case tea.MouseActionMotion:
		if m.drag != nil && msg.Button == tea.MouseButtonLeft {
			m.drag.moved = m.drag.moved || m.drag.x != msg.X || m.drag.y != msg.Y
			m.drag.x, m.drag.y = msg.X, msg.Y
		}
	case tea.MouseActionRelease:
		if msg.Button != tea.MouseButtonLeft && msg.Button != tea.MouseButtonNone {
			return nil
		}
		drag := m.drag
		m.drag = nil
		if drag != nil && index >= 0 && index != drag.from {
			return m.begin(job{"Copy", drag.parent, m.dropTarget(index, hit), m.cfg.Share, drag.names})
		}
	case tea.MouseActionPress:
		if index < 0 {
			return nil
		}
		p := &m.panes[index]
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			p.cursor = max(0, p.cursor-3)
		case tea.MouseButtonWheelDown:
			p.cursor = min(max(0, len(p.visible)-1), p.cursor+3)
		case tea.MouseButtonLeft:
			m.active = index
			m.drag = nil
			if hit >= 0 {
				p.cursor = hit
				name := p.current()
				names := []string{name}
				if p.selected[name] {
					names = p.targets()
				}
				m.drag = &paneDrag{from: index, x: msg.X, y: msg.Y, parent: p.path, name: name, names: names}
			}
		}
	}
	return nil
}

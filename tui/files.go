package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sys/unix"
)

type filePane struct {
	path, filter  string
	entries       []os.DirEntry
	visible       []os.DirEntry
	selected      map[string]bool
	cursor        int
	err           error
	searchContext context.Context
	cancelSearch  context.CancelFunc
	matches       []os.DirEntry
	searchNote    string
	searchField   bool
}

func (p *filePane) current() string {
	if len(p.visible) == 0 {
		return ""
	}
	return p.visible[p.cursor].Name()
}

func (p *filePane) applyFilter(keep string) {
	p.visible = p.entries
	if p.filter != "" {
		needle := searchText(p.filter)
		p.visible = nil
		for _, entry := range p.entries {
			if strings.Contains(searchText(entry.Name()), needle) {
				p.visible = append(p.visible, entry)
			}
		}
		p.visible = append(p.visible, p.matches...)
	}
	p.cursor = min(p.cursor, max(0, len(p.visible)-1))
	for i, entry := range p.visible {
		if entry.Name() == keep {
			p.cursor = i
			break
		}
	}
}

func (p *filePane) load() {
	keep := p.current()
	p.entries, p.err = os.ReadDir(p.path)
	p.entries = slices.DeleteFunc(p.entries, func(entry os.DirEntry) bool {
		return strings.HasPrefix(entry.Name(), ".oxidrop-transfer-")
	})
	slices.SortFunc(p.entries, func(a, b os.DirEntry) int {
		if a.IsDir() != b.IsDir() {
			if a.IsDir() {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name()))
	})
	present := make(map[string]bool, len(p.selected))
	for _, entry := range p.entries {
		if p.selected[entry.Name()] {
			present[entry.Name()] = true
		}
	}
	p.selected = present
	p.applyFilter(keep)
}

func (p *filePane) open(path string) {
	if p.cancelSearch != nil {
		p.cancelSearch()
	}
	*p = filePane{path: path}
	p.load()
}

type searchEntry struct {
	os.DirEntry
	relative string
}

func (e searchEntry) Name() string { return e.relative }

type searchResult struct {
	pane    int
	ctx     context.Context
	entries []os.DirEntry
	note    string
}

func searchText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func walkMatches(ctx context.Context, tree fs.FS, excluded, query string, emit func([]os.DirEntry)) string {
	const limit = 1000
	queue := []string{"."}
	needle := searchText(query)
	found, unreadable := 0, 0
	for len(queue) > 0 && ctx.Err() == nil {
		dir := queue[0]
		queue[0] = ""
		queue = queue[1:]
		children, err := fs.ReadDir(tree, dir)
		if err != nil {
			if dir == "." {
				return "Subfolders unavailable"
			}
			unreadable++
		}
		var batch []os.DirEntry
		for _, entry := range children {
			if ctx.Err() != nil {
				return ""
			}
			path := filepath.Join(dir, entry.Name())
			if path == excluded || strings.HasPrefix(entry.Name(), ".oxidrop-transfer-") {
				continue
			}
			if entry.IsDir() {
				queue = append(queue, path)
			}
			if dir != "." && (entry.IsDir() || entry.Type().IsRegular()) && strings.Contains(searchText(entry.Name()), needle) {
				if found == limit {
					if len(batch) > 0 {
						emit(batch)
					}
					return fmt.Sprintf("First %d nested matches", limit)
				}
				batch = append(batch, searchEntry{entry, path})
				found++
			}
		}
		if len(batch) > 0 {
			emit(batch)
		}
	}
	if unreadable > 0 {
		return fmt.Sprintf("%d unreadable folders", unreadable)
	}
	return ""
}

func (m *model) search(index int, query string) {
	p := &m.panes[index]
	if p.cancelSearch != nil {
		p.cancelSearch()
	}
	keep := p.current()
	if query != p.filter {
		clear(p.selected)
	}
	p.filter, p.matches, p.searchContext, p.searchNote = query, nil, nil, ""
	p.searchField = query != ""
	p.applyFilter(keep)
	if searchText(query) == "" {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	p.searchContext, p.cancelSearch = ctx, cancel
	folder, share := p.path, m.cfg.Share
	m.tasks.Go(func() {
		timer := time.NewTimer(180 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		root, err := openPane(folder, share)
		if err != nil {
			publish(ctx, m.events, searchResult{pane: index, ctx: ctx, note: "Subfolders unavailable"})
			return
		}
		defer root.Close()
		excluded := ""
		if index == 0 {
			if rel, err := filepath.Rel(folder, share); err == nil && rel != "." && filepath.IsLocal(rel) {
				excluded = rel
			}
		}
		note := walkMatches(ctx, root.FS(), excluded, query, func(entries []os.DirEntry) {
			publish(ctx, m.events, searchResult{pane: index, ctx: ctx, entries: entries})
		})
		publish(ctx, m.events, searchResult{pane: index, ctx: ctx, note: note})
	})
}

func (p *filePane) targets() []string {
	if len(p.selected) == 0 {
		if name := p.current(); name != "" {
			return []string{name}
		}
	}
	var names []string
	for _, entry := range p.visible {
		if p.selected[entry.Name()] {
			names = append(names, entry.Name())
		}
	}
	return names
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00")
}

type job struct {
	action, from, to, share string
	names                   []string
}

type collision struct {
	target string
	reply  chan decision
}

type decision struct{ action, name string }
type transferProgress struct {
	action, name string
	index, total int
}
type jobDone struct {
	completed, skipped int
	errors             []error
}

func runJob(ctx context.Context, request job, events chan<- tea.Msg) {
	result := jobDone{}
	defer func() { publish(ctx, events, result) }()
	var src *os.Root
	var err error
	if request.action != "Paste" {
		src, err = openPane(request.from, request.share)
		if err != nil {
			result.errors = append(result.errors, err)
			return
		}
		defer src.Close()
	}
	var dst *os.Root
	if request.action != "Delete" {
		dst, err = openPane(request.to, request.share)
		if err != nil {
			result.errors = append(result.errors, err)
			return
		}
		defer dst.Close()
	}

	for i, name := range request.names {
		if ctx.Err() != nil {
			return
		}
		publish(ctx, events, transferProgress{request.action, name, i + 1, len(request.names)})
		err = func() error {
			source, base := src, filepath.Base(name)
			if request.action == "Paste" {
				source, err = openPane(filepath.Dir(name), request.share)
				if err != nil {
					return err
				}
				defer source.Close()
			} else {
				if !filepath.IsLocal(name) || !validName(base) {
					return errors.New("invalid source path")
				}
				if dir := filepath.Dir(name); dir != "." {
					source, err = src.OpenRoot(dir)
					if err != nil {
						return err
					}
					defer source.Close()
				}
			}
			if request.action == "Delete" {
				return source.RemoveAll(base)
			}
			return transfer(ctx, source, dst, base, request.action == "Move", func(target string) decision {
				reply := make(chan decision, 1)
				publish(ctx, events, collision{target, reply})
				select {
				case choice := <-reply:
					return choice
				case <-ctx.Done():
					return decision{action: "skip"}
				}
			})
		}()
		switch {
		case errors.Is(err, errSkipped):
			result.skipped++
		case err != nil:
			result.errors = append(result.errors, fmt.Errorf("%s: %w", name, err))
		default:
			result.completed++
		}
	}
}

var errSkipped = errors.New("skipped")

func transfer(ctx context.Context, src, dst *os.Root, name string, move bool, ask func(string) decision) (result error) {
	sourceInfo, err := src.Lstat(name)
	if err != nil {
		return err
	}
	srcDir, err := src.Open(".")
	if err != nil {
		return err
	}
	defer srcDir.Close()
	dstDir, err := dst.Open(".")
	if err != nil {
		return err
	}
	defer dstDir.Close()

	target := name
	var staged *os.Root
	var snapshot map[string]os.FileInfo
	if move {
		snapshot = make(map[string]os.FileInfo)
	}
	keepStage := false
	var stageDir *os.File
	stage := ""
	defer func() {
		if stageDir != nil {
			stageDir.Close()
		}
		if staged != nil {
			staged.Close()
		}
		if stage != "" && !keepStage {
			if err := dst.RemoveAll(stage); err != nil {
				result = errors.Join(result, fmt.Errorf("could not clean up %s: %w", filepath.Join(dst.Name(), stage), err))
			}
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		from, to := filepath.Join(src.Name(), name), filepath.Join(dst.Name(), target)
		if from == to || strings.HasPrefix(to, from+"/") || strings.HasPrefix(from, to+"/") {
			return errors.New("source and destination overlap")
		}
		mode := uint(unix.RENAME_NOREPLACE)
		existing, err := dst.Lstat(target)
		switch {
		case err == nil:
			if os.SameFile(sourceInfo, existing) {
				return errors.New("source and destination are the same file")
			}
			switch choice := ask(target); choice.action {
			case "overwrite":
				mode = unix.RENAME_EXCHANGE
			case "rename":
				if !validName(choice.name) {
					return errors.New("invalid destination name")
				}
				target = choice.name
				continue
			default:
				return errSkipped
			}
		case !errors.Is(err, os.ErrNotExist):
			return err
		}

		if move && mode == unix.RENAME_NOREPLACE && stage == "" {
			err = rename(srcDir, name, dstDir, target, mode)
			switch {
			case err == nil:
				return nil
			case errors.Is(err, unix.EEXIST):
				continue
			case !errors.Is(err, unix.EXDEV):
				return err
			}
		}
		if stage == "" {
			candidate := ".oxidrop-transfer-" + rand.Text()
			if err := dst.Mkdir(candidate, 0700); err != nil {
				return err
			}
			stage = candidate
			staged, err = dst.OpenRoot(stage)
			if err != nil {
				return err
			}
			stageDir, err = staged.Open(".")
			if err != nil {
				return err
			}
			if err := copyEntry(ctx, src, name, staged, "item", snapshot); err != nil {
				return err
			}
		}
		if mode == unix.RENAME_EXCHANGE {
			now, err := dst.Lstat(target)
			if errors.Is(err, os.ErrNotExist) || (err == nil && !unchanged(existing, now)) {
				continue
			}
			if err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		stagedInfo, err := staged.Lstat("item")
		if err != nil {
			return err
		}
		err = rename(stageDir, "item", dstDir, target, mode)
		if errors.Is(err, unix.EEXIST) || (mode == unix.RENAME_EXCHANGE && errors.Is(err, unix.ENOENT)) {
			continue
		}
		if err != nil {
			return err
		}
		if mode == unix.RENAME_EXCHANGE {
			prior, err := staged.Lstat("item")
			if err != nil || !unchanged(existing, prior) {
				current, err := dst.Lstat(target)
				if err == nil && unchanged(stagedInfo, current) && rename(stageDir, "item", dstDir, target, mode) == nil {
					continue
				}
				keepStage = true
				return fmt.Errorf("destination changed during overwrite; prior entry saved at %s", filepath.Join(dst.Name(), stage, "item"))
			}
		}
		if move {
			return removeMovedSource(src, srcDir, name, snapshot)
		}
		return nil
	}
}

// Claim the source before deleting it so a replacement at its old name is safe.
func removeMovedSource(src *os.Root, srcDir *os.File, name string, snapshot map[string]os.FileInfo) error {
	hold := ".oxidrop-transfer-" + rand.Text()
	if err := src.Mkdir(hold, 0700); err != nil {
		return fmt.Errorf("destination saved; could not set aside source: %w", err)
	}
	recoverEntry := filepath.Join(hold, name)
	cleanup := true
	defer func() {
		if cleanup {
			src.Remove(hold)
		}
	}()
	if err := rename(srcDir, name, srcDir, recoverEntry, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("destination saved; could not set aside source: %w", err)
	}
	for path, before := range snapshot {
		now, err := src.Lstat(filepath.Join(hold, path))
		if err == nil && unchanged(before, now) {
			continue
		}
		if err := rename(srcDir, recoverEntry, srcDir, name, unix.RENAME_NOREPLACE); err != nil {
			cleanup = false
			return fmt.Errorf("destination saved; changed source retained at %s", filepath.Join(src.Name(), recoverEntry))
		}
		return errors.New("destination saved; source changed during the move and was retained")
	}
	if err := src.RemoveAll(recoverEntry); err != nil {
		cleanup = false
		return fmt.Errorf("destination saved; source cleanup failed at %s: %w", filepath.Join(src.Name(), recoverEntry), err)
	}
	return nil
}

func openPane(path, share string) (*os.Root, error) {
	relative, err := filepath.Rel(share, path)
	if err != nil || !filepath.IsLocal(relative) {
		return os.OpenRoot(path)
	}
	root, err := os.OpenRoot(share)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenRoot(relative)
}

func rename(from *os.File, name string, to *os.File, target string, flags uint) error {
	return unix.Renameat2(int(from.Fd()), name, int(to.Fd()), target, flags)
}

func unchanged(before, after os.FileInfo) bool {
	return os.SameFile(before, after) && before.ModTime() == after.ModTime() && before.Size() == after.Size()
}

func copyEntry(ctx context.Context, src *os.Root, name string, dst *os.Root, target string, snapshot map[string]os.FileInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := src.Lstat(name)
	if err != nil {
		return err
	}
	if snapshot != nil {
		snapshot[name] = info
	}
	switch {
	case info.IsDir():
		if err := dst.Mkdir(target, 0700); err != nil {
			return err
		}
		children, err := fs.ReadDir(src.FS(), name)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := copyEntry(ctx, src, filepath.Join(name, child.Name()), dst, filepath.Join(target, child.Name()), snapshot); err != nil {
				return err
			}
		}
		if err := dst.Chmod(target, info.Mode().Perm()); err != nil {
			return err
		}
	case info.Mode()&os.ModeSymlink != 0:
		link, err := src.Readlink(name)
		if err != nil {
			return err
		}
		return dst.Symlink(link, target)
	case info.Mode().IsRegular():
		if err := copyFile(ctx, src, name, dst, target, info); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported file type: %s", info.Mode())
	}
	now, err := src.Lstat(name)
	if err != nil || !unchanged(info, now) {
		return errors.New("source changed during copy")
	}
	return dst.Chtimes(target, info.ModTime(), info.ModTime())
}

func copyFile(ctx context.Context, src *os.Root, name string, dst *os.Root, target string, info os.FileInfo) error {
	in, err := src.OpenFile(name, unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	opened, err := in.Stat()
	if err != nil || !unchanged(info, opened) {
		return errors.New("source changed during copy")
	}
	out, err := dst.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, cancelReader{ctx, in})
	if copyErr == nil {
		copyErr = out.Chmod(info.Mode().Perm())
	}
	return errors.Join(copyErr, out.Close())
}

type cancelReader struct {
	ctx context.Context
	in  io.Reader
}

func (r cancelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.in.Read(p)
}

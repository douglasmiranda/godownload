package ui

import (
	"sync"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

type taskBar struct {
	bar   *mpb.Bar
	total int64
}

// ProgressTracker implements per-task file-count progress bars via mpb.
type ProgressTracker struct {
	p     *mpb.Progress
	mu    sync.Mutex
	tasks map[string]*taskBar
}

func newProgressTracker() *ProgressTracker {
	return &ProgressTracker{
		p:     mpb.New(mpb.WithWidth(40)),
		tasks: make(map[string]*taskBar),
	}
}

func (t *ProgressTracker) TaskStarted(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.tasks[name]; ok {
		return
	}

	bar := t.p.AddBar(0,
		mpb.PrependDecorators(
			decor.Name(name+":", decor.WC{C: decor.DindentRight | decor.DextraSpace}),
		),
		mpb.AppendDecorators(
			decor.CountersNoUnit("%d / %d", decor.WCSyncWidth),
			decor.Name(" | ", decor.WCSyncWidth),
			decor.Percentage(decor.WCSyncWidth),
		),
	)
	t.tasks[name] = &taskBar{bar: bar}
}

func (t *ProgressTracker) TaskTotal(name string, total int) {
	if total <= 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	tb, ok := t.tasks[name]
	if !ok {
		return
	}
	tb.total = int64(total)
	tb.bar.SetCurrent(0)
	tb.bar.SetTotal(int64(total), false)
}

func (t *ProgressTracker) TaskFileDone(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	tb, ok := t.tasks[name]
	if !ok {
		return
	}
	tb.bar.Increment()
}

func (t *ProgressTracker) TaskFinished(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	tb, ok := t.tasks[name]
	if !ok {
		return
	}
	if tb.total > 0 {
		remaining := tb.total - tb.bar.Current()
		if remaining > 0 {
			tb.bar.IncrBy(int(remaining))
		}
	}
	tb.bar.SetTotal(tb.bar.Current(), true)
	delete(t.tasks, name)
}

func (t *ProgressTracker) Wait() {
	if t.p != nil {
		t.p.Wait()
	}
}

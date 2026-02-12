package scoped

import (
	"context"
	"sync/atomic"
	"time"
)

// Spawner allows spawning concurrent tasks into a scope.
type Spawner interface {
	// Spawn starts a new concurrent task with the given name.
	// The task function receives a child Spawner allowing it to create sub-tasks.
	Spawn(name string, fn TaskFunc)
}

// spawner implements the Spawner interface and manages the lifecycle of tasks.
type spawner struct {
	s    *scope
	open atomic.Bool
}

// Spawn implements Spawner.Spawn.

func (sp *spawner) Spawn(name string, fn TaskFunc) {
	// Check open BEFORE wg.Add to avoid TOCTOU race with finalize()'s wg.Wait().
	if !sp.open.Load() {
		panic("scoped: Spawn called after scope shutdown")
	}

	sp.s.wg.Add(1)

	info := TaskInfo{Name: name}

	go func() {
		defer sp.s.wg.Done()

		// semaphore
		if sp.s.sem != nil {
			select {
			case sp.s.sem <- struct{}{}:
				defer func() { <-sp.s.sem }()
			case <-sp.s.ctx.Done():
				// Context cancelled while waiting for semaphore slot.
				// Don't record as task error — the real cause is already recorded.
				return
			}
		}

		if sp.s.ctx.Err() != nil {
			// Context already cancelled, skip execution silently.
			return
		}

		//child spawner is valid only for the lifetime of the task;
		// spawning after the task function returns will panic.
		child := &spawner{
			s:    sp.s,
			open: atomic.Bool{},
		}
		child.open.Store(true)

		start := time.Now()
		// Hooks run inside exec() so panics are caught by recovery.
		err := sp.s.exec(func(ctx context.Context) error {
			if sp.s.cfg.onStart != nil {
				sp.s.cfg.onStart(info)
			}
			taskErr := fn(ctx, child)
			return taskErr
		})
		elapsed := time.Since(start)

		child.close()

		if sp.s.cfg.onDone != nil {
			// onDone runs outside exec — a panic here is intentionally
			// unrecovered (observability hook must not panic).
			sp.s.cfg.onDone(info, err, elapsed)
		}

		if err != nil {
			sp.s.recordError(info, err)
		}
	}()
}

// close marks the spawner as closed, preventing further Spawn calls.
func (sp *spawner) close() {
	sp.open.Store(false)
}

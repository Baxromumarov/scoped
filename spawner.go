package scoped

import (
	"context"
	"sync"
	"time"
)

// Spawner allows spawning concurrent tasks into a scope.
type Spawner interface {
	// Spawn starts a new concurrent task with the given name.
	//
	// The task function receives a child [Spawner] allowing it to create sub-tasks.
	// The child Spawner is only valid for the lifetime of the task function; storing
	// it and calling Spawn after the task returns will panic.
	Spawn(name string, fn TaskFunc)
}

// spawner implements the Spawner interface and manages the lifecycle of tasks.
type spawner struct {
	s    *scope
	mu   sync.Mutex // guards open
	open bool
}

// Spawn implements Spawner.Spawn.
func (sp *spawner) Spawn(name string, fn TaskFunc) {
	// Hold mu across the open check and wg.Add to prevent a
	// TOCTOU race:[https://en.wikipedia.org/wiki/Time-of-check_to_time-of-use]
	// without the lock, close() + finalize()'s wg.Wait() could run between
	// the open check and wg.Add, causing a WaitGroup panic.

	sp.mu.Lock()
	if !sp.open {
		sp.mu.Unlock()
		panic("scoped: Spawn called after scope shutdown")
	}

	sp.s.wg.Add(1)
	sp.s.totalSpawned.Add(1)
	sp.s.activeTasks.Add(1)
	sp.mu.Unlock()

	info := TaskInfo{Name: name}

	go func() {
		defer sp.s.activeTasks.Add(-1)
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
			sp.s.emitEvent(TaskEvent{Kind: EventCancelled, Task: info})
			return
		}

		// Child spawner is valid only for the lifetime of the task;
		// spawning after the task function returns will panic.
		child := &spawner{
			s:    sp.s,
			open: true,
		}

		needsTiming := sp.s.cfg.onDone != nil || sp.s.cfg.onEvent != nil

		var start time.Time
		if needsTiming {
			start = time.Now()
		}

		// Emit started event and call legacy onStart hook.
		if sp.s.cfg.onStart != nil || sp.s.cfg.onEvent != nil {
			sp.s.emitEvent(TaskEvent{
				Kind: EventStarted,
				Task: info,
			})
		}

		// Hooks run inside exec() so panics are caught by recovery.
		err := sp.s.exec(
			func(ctx context.Context) error {
				if sp.s.cfg.onStart != nil {
					sp.s.cfg.onStart(info)
				}
				taskErr := fn(ctx, child)
				return taskErr
			},
		)

		child.close()

		var elapsed time.Duration
		if needsTiming {
			elapsed = time.Since(start)
		}

		if sp.s.cfg.onDone != nil {
			sp.s.cfg.onDone(info, err, elapsed)
		}

		// Emit the appropriate completion event.
		sp.s.emitCompletionEvent(info, err, elapsed)

		if err != nil {
			sp.s.recordError(info, err)
		}
	}()
}

// close marks the spawner as closed, preventing further Spawn calls.
func (sp *spawner) close() {
	sp.mu.Lock()
	sp.open = false
	sp.mu.Unlock()
}

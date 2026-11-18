package bot

import (
	"context"
	"sync"
	"time"
)

func (a *App) scheduleRefresh(ctx context.Context) {
	for {
		next := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			a.refreshRooms(ctx)
		}
	}
}

func (a *App) refreshRooms(ctx context.Context) {
	var wg sync.WaitGroup
	for _, dep := range a.departments {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rooms, err := a.roomClient.Load(ctx, dep)
			if err != nil {
				a.logger.Error("department refresh failed", "id", dep.ID, "name", dep.Name, "error", err)
				return
			}
			a.departMu.Lock()
			dep.Rooms = rooms
			a.departMu.Unlock()
		}()
	}
	wg.Wait()
}

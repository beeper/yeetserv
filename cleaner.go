package main

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"

	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

type OKResponse struct {
	Removed uint64 `json:"removed"`
	Skipped uint64 `json:"skipped"`
	Failed  uint64 `json:"failed"`
}

func cleanRooms(ctx context.Context, client *mautrix.Client) (*OKResponse, error) {
	reqLog := ctx.Value(logContextKey).(log.Logger)
	reqLog.Infoln(client.UserID, "requested a room cleanup")
	rooms, err := GetRoomList(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to get room list: %w", err)
	}
	reqLog.Debugln("Found", len(rooms), "rooms")

	var resp OKResponse
	var wg sync.WaitGroup
	wg.Add(len(rooms))
	queue := make(chan id.RoomID)
	for i := 1; i <= cfg.ThreadCount; i++ {
		threadContext := context.WithValue(ctx, logContextKey, reqLog.Sub(fmt.Sprintf("Thread-%d", i)))
		go cleanRoomsThread(threadContext, client, queue, &wg, &resp)
	}
	for _, roomID := range rooms {
		select {
		case queue <- roomID:
		case <-ctx.Done():
			reqLog.Warnfln("Room cleanup for %s was canceled before it completed. Status: %+v", client.UserID, resp)
			close(queue)
			return &resp, ctx.Err()
		}
	}
	wg.Wait()
	close(queue)
	reqLog.Infofln("Room cleanup for %s completed successfully. Status: %+v", client.UserID, resp)
	return &resp, nil
}

func cleanRoomsThread(ctx context.Context, client *mautrix.Client, queue <-chan id.RoomID, wg *sync.WaitGroup, resp *OKResponse) {
	reqLog := ctx.Value(logContextKey).(log.Logger)
	defer func() {
		err := recover()
		if err != nil {
			reqLog.Errorfln("Panic in room cleaning thread for %s: %v\n%s", client.UserID, err, debug.Stack())
		}
	}()
	for {
		select {
		case roomID, ok := <-queue:
			if !ok {
				return
			}
			allowed, err := cleanRoom(ctx, client, roomID)
			if err != nil {
				reqLog.Warnfln("Failed to clean up %s: %v", roomID, err)
				atomic.AddUint64(&resp.Failed, 1)
			} else if allowed {
				atomic.AddUint64(&resp.Removed, 1)
			} else {
				atomic.AddUint64(&resp.Skipped, 1)
			}
			wg.Done()
		case <-ctx.Done():
			return
		}
	}
}

func cleanRoom(ctx context.Context, client *mautrix.Client, roomID id.RoomID) (allowed bool, err error) {
	reqLog := ctx.Value(logContextKey).(log.Logger)
	defer func() {
		panicErr := recover()
		if panicErr != nil {
			err = fmt.Errorf("panic while cleaning %s for %s: %v\n%s", roomID, client.UserID, panicErr, debug.Stack())
		}
	}()

	usersToKick, permissionErr := IsAllowedToCleanRoom(ctx, client, roomID)
	if permissionErr != nil {
		reqLog.Debugfln("Skipping room %s as cleaning is not allowed: %v", roomID, permissionErr)
		return
	}
	allowed = true

	err = PushLeaveQueue(ctx, roomID, usersToKick)
	if err == nil {
		reqLog.Debugfln("Room %s queued for leaving", roomID)
	}

	return
}

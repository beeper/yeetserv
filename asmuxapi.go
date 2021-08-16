package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

// asmuxDeleteRoom tells asmux to forget about a specific room.
func asmuxDeleteRoom(ctx context.Context, roomID id.RoomID) error {
	if len(cfg.AsmuxAccessToken) == 0 {
		return fmt.Errorf("asmux access token not set")
	} else if cfg.AsmuxMainURL == nil {
		return fmt.Errorf("asmux main URL not set")
	} else if cfg.DryRun {
		time.Sleep(200 * time.Millisecond)
		return nil
	}
	roomURL := mautrix.BuildURL(cfg.AsmuxMainURL, "_matrix", "asmux", "room", roomID).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, roomURL, nil)
	if err != nil {
		return fmt.Errorf("failed to prepare request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.AsmuxAccessToken))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}
	return nil
}

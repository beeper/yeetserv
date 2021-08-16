package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v4"
	log "maunium.net/go/maulogger/v2"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

// GetRoomList returns the list of rooms that the given client wants to clean.
//
// If the ASMUX_DATABASE_URL env var is set, this reads the asmux database. Otherwise this uses the /joined_rooms API.
func GetRoomList(ctx context.Context, client *mautrix.Client) ([]id.RoomID, error) {
	if len(cfg.AsmuxDatabaseURL) > 0 {
		return getRoomsFromAsmuxDatabase(ctx, client)
	} else {
		return getJoinedRooms(client)
	}
}

// getJoinedRooms uses the /joined_rooms endpoint to find all rooms a user is in.
func getJoinedRooms(client *mautrix.Client) ([]id.RoomID, error) {
	resp, err := client.JoinedRooms()
	if err != nil {
		return nil, err
	}
	return resp.JoinedRooms, nil
}

// getRoomsFromAsmuxDatabase reads a mautrix-asmux database to find rooms that are routed to a specific appservice.
func getRoomsFromAsmuxDatabase(ctx context.Context, client *mautrix.Client) ([]id.RoomID, error) {
	reqLog := ctx.Value(logContextKey).(log.Logger)
	bridgeUserLocalpart, bridgeName, _, err := parseBridgeName(client.UserID)
	if err != nil {
		return nil, err
	}
	conn, err := pgx.Connect(ctx, cfg.AsmuxDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to asmux database: %w", err)
	}
	defer func() {
		closeErr := conn.Close(ctx)
		if closeErr != nil {
			reqLog.Warnln("Failed to close asmux database connection:", err)
		}
	}()
	rows, err := conn.Query(ctx, "SELECT id FROM room WHERE owner=(SELECT id FROM appservice WHERE owner=$1 AND prefix=$2 AND deleted=false)", bridgeUserLocalpart, bridgeName)
	if err != nil {
		return nil, fmt.Errorf("failed to query rooms in asmux database: %w", err)
	}
	var rooms []id.RoomID
	for i := 0; rows.Next(); i++ {
		rooms = append(rooms, "")
		err = rows.Scan(&rooms[i])
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
	}
	return rooms, nil
}

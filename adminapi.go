package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

type ReqDeleteRoom struct {
	RoomID id.RoomID `json:"-"`
	Purge  bool      `json:"purge"`
}

type RespDeleteRoom struct {
	KickedUsers       []id.UserID    `json:"kicked_users"`
	FailedToKickUsers []id.UserID    `json:"failed_to_kick_users"`
	LocalAliases      []id.RoomAlias `json:"local_aliases"`
	NewRoomID         id.RoomID      `json:"new_room_id,omitempty"`
}

var fakeDeleteResponse = RespDeleteRoom{
	KickedUsers:       []id.UserID{"@fake:user.com"},
	FailedToKickUsers: []id.UserID{},
	LocalAliases:      []id.RoomAlias{},
}

// https://matrix-org.github.io/synapse/latest/admin_api/rooms.html#delete-room-api
func adminDeleteRoom(ctx context.Context, req ReqDeleteRoom) (*RespDeleteRoom, error) {
	var resp RespDeleteRoom
	var err error
	if cfg.DryRun {
		select {
		case <-time.After(time.Duration(rand.Float64() * 5 * float64(time.Second))):
			resp = fakeDeleteResponse
		case <-ctx.Done():
			err = fmt.Errorf("context errored in dry run mode: %w", ctx.Err())
		}
	} else {
		url := adminClient.BuildBaseURL("_synapse", "admin", "v1", "rooms", req.RoomID)
		_, err = adminClient.MakeFullRequest(mautrix.FullRequest{
			Method:       http.MethodDelete,
			URL:          url,
			RequestJSON:  &req,
			ResponseJSON: &resp,
			Context:      ctx,
		})
	}
	return &resp, err
}

type RespListMembers struct {
	Members []id.UserID `json:"members"`
	Total   int         `json:"total"`
}

// https://matrix-org.github.io/synapse/latest/admin_api/rooms.html#room-members-api
func adminListRoomMembers(ctx context.Context, roomID id.RoomID) ([]id.UserID, error) {
	url := adminClient.BuildBaseURL("_synapse", "admin", "v1", "rooms", roomID, "members")
	var resp RespListMembers
	_, err := adminClient.MakeFullRequest(mautrix.FullRequest{
		Method:       http.MethodGet,
		URL:          url,
		ResponseJSON: &resp,
		Context:      ctx,
	})
	if err != nil {
		return nil, err
	}
	return resp.Members, nil
}

type ReqAdminLogin struct {
	ValidUntilMS int64 `json:"valid_until_ms"`
	UserID id.UserID `json:"-"`
}

type RespAdminLogin struct {
	AccessToken string `json:"access_token"`
}

// https://matrix-org.github.io/synapse/latest/admin_api/user_admin_api.html#login-as-a-user
func adminLogin(ctx context.Context, req ReqAdminLogin) (*RespAdminLogin, error) {
	url := adminClient.BuildBaseURL("_synapse", "admin", "v1", "users", req.UserID, "login")
	var resp RespAdminLogin
	_, err := adminClient.MakeFullRequest(mautrix.FullRequest{
		Method:       http.MethodDelete,
		URL:          url,
		RequestJSON:  &req,
		ResponseJSON: &resp,
		Context:      ctx,
	})
	return &resp, err
}

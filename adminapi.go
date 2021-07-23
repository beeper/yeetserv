package main

import (
	"context"
	"net/http"

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

func adminDeleteRoom(ctx context.Context, req *ReqDeleteRoom) (*RespDeleteRoom, error) {
	url := adminClient.BuildBaseURL("_synapse", "admin", "v1", "rooms", req.RoomID)
	var resp RespDeleteRoom
	_, err := adminClient.MakeFullRequest(mautrix.FullRequest{
		Method:       http.MethodDelete,
		URL:          url,
		RequestJSON:  req,
		ResponseJSON: &resp,
		Context:      ctx,
	})
	return &resp, err
}

type RespListMembers struct {
	Members []id.UserID `json:"members"`
	Total   int         `json:"total"`
}

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

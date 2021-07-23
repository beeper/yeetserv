package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// AllowedLocalpartRegex is the regex matching localparts of users who are allowed to use the cleanup service.
// The default regex here matches mautrix-asmux bridge bots and the idea is to call the service with the as_token.
var AllowedLocalpartRegex = regexp.MustCompile("^_([a-z0-9-]+)_([a-z0-9-]+)_bot$")

// IsAllowedToUseService checks if the given user can use this cleanup service.
func IsAllowedToUseService(ctx context.Context, client *mautrix.Client, whoami *mautrix.RespWhoami) error {
	client.UserID = whoami.UserID
	localpart, _, err := client.UserID.Parse()
	if err != nil {
		return fmt.Errorf("failed to parse user ID: %w", err)
	} else if !AllowedLocalpartRegex.MatchString(localpart) {
		return fmt.Errorf("only bridge bots can clean up rooms")
	}
	return nil
}

func parseBridgeName(userID id.UserID) (bridgeUserLocalpart, bridgeName, homeserver string, err error) {
	var botLocalpart string
	// Parsing and the allowed localpart check should never fail at this point since
	// they're also checked in IsAllowedToUseService, but handle them just in case anyway.
	if botLocalpart, homeserver, err = userID.Parse(); err != nil {
		err = fmt.Errorf("failed to parse user ID: %w", err)
	} else if parts := AllowedLocalpartRegex.FindStringSubmatch(botLocalpart); len(parts) != 3 {
		err = fmt.Errorf("didn't get expected number of parts from parsing user ID localpart")
	} else {
		bridgeUserLocalpart = parts[1]
		bridgeName = parts[2]
	}
	return
}

// IsAllowedToCleanRoom checks if the given client has sufficient permissions in the room to include it in the cleanup.
func IsAllowedToCleanRoom(ctx context.Context, client *mautrix.Client, roomID id.RoomID) error {
	bridgeUserLocalpart, bridgeName, homeserver, err := parseBridgeName(client.UserID)
	if err != nil {
		return err
	}
	// The localpart prefix for ghost users managed by the bridge.
	bridgeGhostPrefix := fmt.Sprintf("_%s_%s_", bridgeUserLocalpart, bridgeName)

	var randomBridgeGhostInRoom id.UserID
	members, err := adminListRoomMembers(ctx, roomID)
	if err != nil {
		return fmt.Errorf("failed to get members of %s: %w", roomID, err)
	}
	// Make sure the room doesn't contain anyone except the user of the bridge, the bridge bot and bridge ghosts.
	for _, member := range members {
		memberLocalpart, memberHomeserver, _ := member.Parse()
		if memberHomeserver != homeserver {
			return fmt.Errorf("room contains member '%s' from other homeserver '%s' (expected '%s')", member, memberHomeserver, homeserver)
		} else if memberLocalpart != bridgeUserLocalpart {
			if strings.HasPrefix(memberLocalpart, bridgeGhostPrefix) {
				randomBridgeGhostInRoom = member
			} else {
				return fmt.Errorf("room contains member '%s' that is not the bridge user nor a bridge ghost (expected '%s' or prefix '%s')", member, bridgeUserLocalpart, bridgeGhostPrefix)
			}
		}
	}

	// Copy the client and set AppServiceUserID to sure the power level request
	// is always done by a user in the room.
	appserviceClient := &mautrix.Client{
		AppServiceUserID: randomBridgeGhostInRoom,

		AccessToken:   client.AccessToken,
		UserAgent:     client.UserAgent,
		HomeserverURL: client.HomeserverURL,
		UserID:        client.UserID,
		Client:        client.Client,
		Prefix:        client.Prefix,
		Store:         client.Store,
	}

	var pl event.PowerLevelsEventContent
	err = appserviceClient.StateEvent(roomID, event.StatePowerLevels, "", &pl)
	if err != nil {
		return fmt.Errorf("failed to get power levels of %s: %w", roomID, err)
	}
	// Make sure that the bridge bot or at least one bridged user has PL 100.
	if pl.GetUserLevel(client.UserID) < 100 {
		found := false
		for userID, level := range pl.Users {
			if level >= 100 && strings.HasPrefix(userID.String(), "@"+bridgeGhostPrefix) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("room doesn't have any bridge user with admin power level")
		}
	}

	// All good, room is safe to delete.
	return nil
}

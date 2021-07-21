package main

import (
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
func IsAllowedToUseService(client *mautrix.Client, whoami *mautrix.RespWhoami) error {
	client.UserID = whoami.UserID
	localpart, _, err := client.UserID.Parse()
	if err != nil {
		return fmt.Errorf("failed to parse user ID: %w", err)
	} else if !AllowedLocalpartRegex.MatchString(localpart) {
		return fmt.Errorf("only bridge bots can clean up rooms")
	}
	return nil
}

// IsAllowedToCleanRoom checks if the given client has sufficient permissions in the room to include it in the cleanup.
func IsAllowedToCleanRoom(client *mautrix.Client, roomID id.RoomID) error {
	// Parsing and the allowed localpart check should never fail at this point since
	// they're also checked in IsAllowedToUseService, but handle them just in case anyway.
	localpart, homeserver, err := client.UserID.Parse()
	if err != nil {
		return fmt.Errorf("failed to parse user ID: %w", err)
	}
	parts := AllowedLocalpartRegex.FindStringSubmatch(localpart)
	if len(parts) != 3 {
		return fmt.Errorf("didn't get expected number of parts from parsing user ID localpart")
	}
	// The localpart of the user who was using the bridge.
	bridgeUserLocalpart := parts[1]
	// The localpart prefix for ghost users managed by the bridge.
	bridgeGhostPrefix := fmt.Sprintf("_%s_%s_", bridgeUserLocalpart, parts[2])

	var pl event.PowerLevelsEventContent
	err = client.StateEvent(roomID, event.StatePowerLevels, "", &pl)
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

	members, err := client.JoinedMembers(roomID)
	if err != nil {
		return fmt.Errorf("failed to get members of %s: %w", roomID, err)
	}
	// Make sure the room doesn't contain anyone except the user of the bridge, the bridge bot and bridge ghosts.
	for member := range members.Joined {
		memberLocalpart, memberHomeserver, _ := member.Parse()
		if memberHomeserver != homeserver {
			return fmt.Errorf("room contains member '%s' from other homeserver '%s' (expected '%s')", member, memberHomeserver, homeserver)
		} else if memberLocalpart != bridgeUserLocalpart && !strings.HasPrefix(memberLocalpart, bridgeGhostPrefix) {
			return fmt.Errorf("room contains member '%s' that is not the bridge user nor a bridge ghost (expected '%s' or prefix '%s')", member, bridgeUserLocalpart, bridgeGhostPrefix)
		}
	}

	// All good, room is safe to delete.
	return nil
}

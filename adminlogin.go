package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

type adminLoginSession struct {
	*mautrix.Client
	sync.Mutex
	ValidUntil time.Time
}

// AdminLoginLifetime specifies how long user access tokens created through the admin API should be valid.
const AdminLoginLifetime = 2 * time.Hour

// AdminLoginMinTimeLeft specifies how long an access token must have left to live before a new access token is created.
const AdminLoginMinTimeLeft = 10 * time.Minute

// sessionsLock is the mutex used to lock reading/writing the sessions map.
var sessionsLock sync.Mutex

// sessions contains active user access tokens.
var sessions = make(map[id.UserID]*adminLoginSession)

func getAdminLoginSession(userID id.UserID) *adminLoginSession {
	sessionsLock.Lock()
	sess, ok := sessions[userID]
	if !ok {
		sess = &adminLoginSession{}
		sessions[userID] = sess
	}
	sessionsLock.Unlock()
	return sess
}

// AdminLogin gets an access token for the given user using the admin API.
//
// If there's an existing valid access token, that token is returned.
// Otherwise, a new token is created and cached for future use.
func AdminLogin(ctx context.Context, userID id.UserID) (client *mautrix.Client, err error) {
	reqLog := ctx.Value(logContextKey).(log.Logger)

	sess := getAdminLoginSession(userID)
	sess.Lock()
	defer sess.Unlock()

	if sess.Client != nil && sess.ValidUntil.After(time.Now().Add(AdminLoginMinTimeLeft)) {
		reqLog.Debugfln("Using existing access token for %s (valid until %s)", userID, sess.ValidUntil)
		return sess.Client, nil
	}

	validUntil := time.Now().Add(AdminLoginLifetime)
	reqLog.Debugfln("Requesting admin API to create a new access token for %s (valid until %s)", userID, validUntil)
	resp, err := adminLogin(ctx, ReqAdminLogin{
		UserID:       userID,
		ValidUntilMS: validUntil.Unix() * 1000,
	})
	if err != nil {
		err = fmt.Errorf("failed to request user access token: %w", err)
	} else if client, err = mautrix.NewClient(cfg.SynapseURL, userID, resp.AccessToken); err != nil {
		err = fmt.Errorf("failed to create mautrix client: %w", err)
	} else {
		sess.Client = client
		sess.ValidUntil = validUntil
	}
	return
}

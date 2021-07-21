package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
)

var globalReqID int32
var logContextKey = "com.beeper.maulogger"

var (
	errMissingToken = appservice.Error{
		HTTPStatus: http.StatusUnauthorized,
		ErrorCode:  "M_MISSING_TOKEN",
		Message:    "Missing authorization header",
	}
	errUnknownToken = appservice.Error{
		HTTPStatus: http.StatusUnauthorized,
		ErrorCode:  "M_UNKNOWN_TOKEN",
		Message:    "Unknown authorization token",
	}
	errTokenCheckFail = appservice.Error{
		HTTPStatus: http.StatusUnauthorized,
		ErrorCode:  "M_UNKNOWN_TOKEN",
		Message:    "Failed to check token validity",
	}
	errCleanFailed = appservice.Error{
		HTTPStatus: http.StatusInternalServerError,
		ErrorCode:  "M_UNKNOWN",
		Message:    "An internal error occurred while cleaning rooms",
	}
	errCleanForbidden = appservice.Error{
		HTTPStatus: http.StatusForbidden,
		ErrorCode:  "M_FORBIDDEN",
		Message:    "You are not allowed to use this microservice to clean up rooms",
	}
)

func handleCleanRooms(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(token) == 0 {
		errMissingToken.Write(w)
		return
	}

	reqID := atomic.AddInt32(&globalReqID, 1)
	reqLog := log.Sub("Req").Sub(clientIP(r)).Sub(strconv.Itoa(int(reqID)))
	ctx := context.WithValue(r.Context(), logContextKey, reqLog)

	var whoami *mautrix.RespWhoami
	var resp *OKResponse
	if client, err := mautrix.NewClient(cfg.AsmuxURL, "", token); err != nil {
		reqLog.Warnfln("Failed to create client:", err)
		errTokenCheckFail.Write(w)
	} else if whoami, err = client.Whoami(); errors.Is(err, mautrix.MUnknownToken) {
		reqLog.Debugln("Incorrect token:", err)
		errUnknownToken.Write(w)
	} else if err != nil {
		reqLog.Warnln("Unknown error checking whoami:", err)
		errTokenCheckFail.Write(w)
	} else if err = IsAllowedToUseService(client, whoami); err != nil {
		reqLog.Debugfln("%s asked to clean rooms, but the rules rejected it: %v", client.UserID, err)
		errCleanForbidden.Write(w)
	} else if resp, err = cleanRooms(ctx, client); err != nil {
		reqLog.Errorfln("Failed to clean rooms of %s: %v", client.UserID, err)
		errCleanFailed.Write(w)
	} else {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func clientIP(r *http.Request) string {
	if cfg.TrustForwardHeader {
		fwd := r.Header.Get("X-Forwarded-For")
		if len(fwd) > 0 {
			parts := strings.Split(fwd, ",")
			return strings.TrimSpace(parts[0])
		}
	}
	return r.RemoteAddr
}

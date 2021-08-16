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
	"maunium.net/go/mautrix/id"
)

var globalReqID int32
var logContextKey = "com.beeper.maulogger"

var (
	errNotJSON = appservice.Error{
		HTTPStatus: http.StatusNotAcceptable,
		ErrorCode: appservice.ErrNotJSON,
		Message: "Request body is not JSON",
	}
	errBadJSON = appservice.Error{
		HTTPStatus: http.StatusBadRequest,
		ErrorCode:  appservice.ErrBadJSON,
		Message:    "Request body doesn't contain required keys",
	}
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

func prepareRequest(r *http.Request) (context.Context, log.Logger) {
	reqID := atomic.AddInt32(&globalReqID, 1)
	reqLog := log.Sub("Req").Sub(clientIP(r)).Sub(strconv.Itoa(int(reqID)))
	ctx := context.WithValue(r.Context(), logContextKey, reqLog)
	return ctx, reqLog
}

func verifyToken(ctx context.Context, w http.ResponseWriter, authHeader string) *mautrix.Client {
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if len(token) == 0 {
		errMissingToken.Write(w)
		return nil
	}

	reqLog := ctx.Value(logContextKey).(log.Logger)
	var whoami *mautrix.RespWhoami
	if client, err := mautrix.NewClient(cfg.AsmuxURL, "", token); err != nil {
		reqLog.Warnfln("Failed to create client:", err)
		errTokenCheckFail.Write(w)
	} else if whoami, err = client.Whoami(); errors.Is(err, mautrix.MUnknownToken) {
		reqLog.Debugln("Incorrect token:", err)
		errUnknownToken.Write(w)
	} else if err != nil {
		reqLog.Warnln("Unknown error checking whoami:", err)
		errTokenCheckFail.Write(w)
	} else if err = IsAllowedToUseService(ctx, client, whoami); err != nil {
		reqLog.Debugfln("%s asked to clean rooms, but the rules rejected it: %v", client.UserID, err)
		errCleanForbidden.Write(w)
	} else {
		return client
	}
	return nil
}

func handleCleanAllRooms(w http.ResponseWriter, r *http.Request) {
	ctx, reqLog := prepareRequest(r)
	client := verifyToken(ctx, w, r.Header.Get("Authorization"))
	if client == nil {
		return
	}

	if resp, err := cleanRooms(ctx, client); err != nil {
		reqLog.Errorfln("Failed to clean rooms of %s: %v", client.UserID, err)
		errCleanFailed.Write(w)
	} else {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

type ReqQueueRooms struct {
	RoomIDs []id.RoomID `json:"room_ids"`
}

type RespQueueRooms struct {
	Queued   []id.RoomID `json:"queued"`
	Failed   []id.RoomID `json:"failed"`
	Rejected []id.RoomID `json:"rejected"`
}

func handleQueue(w http.ResponseWriter, r *http.Request) {
	ctx, reqLog := prepareRequest(r)
	client := verifyToken(ctx, w, r.Header.Get("Authorization"))
	if client == nil {
		return
	}

	var req ReqQueueRooms
	err := json.NewDecoder(r.Body).Decode(&req)
	if _, ok := err.(*json.SyntaxError); ok {
		w.Header().Add("Accept", "application/json")
		errNotJSON.Write(w)
		return
	} else if err != nil {
		errBadJSON.Write(w)
		return
	}

	var resp RespQueueRooms
	for _, roomID := range req.RoomIDs {
		_, err = IsAllowedToCleanRoom(ctx, client, roomID)
		if err != nil {
			resp.Rejected = append(resp.Rejected, roomID)
		} else {
			err = PushDeleteQueue(ctx, roomID)
			if err != nil {
				resp.Failed = append(resp.Failed, roomID)
				reqLog.Warnfln("Failed to queue %s for deletion: %v", err)
			} else {
				reqLog.Debugln("Queued", roomID, "for deletion")
				resp.Queued = append(resp.Queued, roomID)
			}
		}
	}

	w.Header().Add("Content-Type", "application/json")
	if len(resp.Queued) > 0 || len(req.RoomIDs) == 0 {
		w.WriteHeader(http.StatusAccepted)
	} else if len(resp.Rejected) > 0 {
		w.WriteHeader(http.StatusForbidden)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
	_ = json.NewEncoder(w).Encode(&resp)
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

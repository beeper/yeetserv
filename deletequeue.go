package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	log "maunium.net/go/maulogger/v2"

	"maunium.net/go/mautrix/id"
)

type LeavingRoom struct {
	RoomID id.RoomID   `json:"roomID"`
	Kick   []id.UserID `json:"kick"`
}

type PendingRoom struct {
	RoomID    id.RoomID `json:"roomID"`
	QueueTime time.Time `json:"queueTime"`
}

var queueLog = log.Sub("Queue")
var leaveQueue chan *LeavingRoom
var deleteQueue chan id.RoomID
var rds *redis.Client
var leaveQueueKey = "yeetserv:leave_queue"
var deleteQueueKey = "yeetserv:delete_queue"
var errorQueueKey = "yeetserv:error_queue"

var promLeaveQueueGauge = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "yeetserv_leave_queue_length",
		Help: "Current length of yeetserv's leave queue",
	},
)
var promDeleteQueueGauge = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "yeetserv_delete_queue_length",
		Help: "Current length of yeetserv's delete queue",
	},
)
var promErrorQueueGauge = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "yeetserv_error_queue_length",
		Help: "Current length of yeetserv's error queue",
	},
)
var promLeaveCounter = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "yeetserv_leave_count",
		Help: "Number of leaves performed",
	},
)
var promDeleteCounter = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "yeetserv_delete_count",
		Help: "Number of deletes performed",
	},
)
var promLeaveSeconds = promauto.NewHistogram(
	prometheus.HistogramOpts{
		Name:    "yeetserv_leave_seconds",
		Help:    "Time taken to leave in seconds",
		Buckets: []float64{10, 30, 60, 300, 600, 1200, 3600},
	},
)
var promDeleteSeconds = promauto.NewHistogram(
	prometheus.HistogramOpts{
		Name:    "yeetserv_delete_seconds",
		Help:    "Time taken to delete in seconds",
		Buckets: []float64{10, 30, 60, 300, 600, 1200, 3600},
	},
)

func initQueue() {
	if len(cfg.RedisURL) > 0 {
		log.Debugln("Initializing redis client")
		redisURL, err := url.Parse(cfg.RedisURL)
		if err != nil {
			log.Fatalln("Bad redis URL:", err)
			os.Exit(4)
		}
		var opts redis.Options
		opts.Addr = redisURL.Host
		opts.Username = redisURL.User.Username()
		opts.Password, _ = redisURL.User.Password()
		rds = redis.NewClient(&opts)

		if cfg.DryRun {
			leaveQueueKey = strings.Replace(leaveQueueKey, ":", ":dry_run:", 1)
			deleteQueueKey = strings.Replace(deleteQueueKey, ":", ":dry_run:", 1)
			errorQueueKey = strings.Replace(errorQueueKey, ":", ":dry_run:", 1)
		}
		log.Debugln("Redis leave queue key:", leaveQueueKey)
		log.Debugln("Redis delete queue key:", deleteQueueKey)
		log.Debugln("Redis error queue key:", errorQueueKey)
	} else {
		leaveQueue = make(chan *LeavingRoom, 8192)
		deleteQueue = make(chan id.RoomID, 8192)
	}
}

func PushLeaveQueue(ctx context.Context, roomID id.RoomID, usersToKick []id.UserID) error {
	leavingRoom := &LeavingRoom{RoomID: roomID, Kick: usersToKick}

	if rds != nil {
		jsonData, err := json.Marshal(leavingRoom)
		if err != nil {
			return fmt.Errorf("failed to marshal %s to redis: %w", roomID, err)
		}
		err = rds.RPush(ctx, leaveQueueKey, jsonData).Err()
		if err != nil {
			return fmt.Errorf("failed to push %s to redis: %w", roomID, err)
		}
		promLeaveQueueGauge.Set(float64(rds.LLen(ctx, leaveQueueKey).Val()))
	} else {
		leaveQueue <- leavingRoom
		promLeaveQueueGauge.Set(float64(len(leaveQueue)))
	}
	return nil
}

func PushDeleteQueue(ctx context.Context, roomID id.RoomID) error {
	if rds != nil {
		pendingRoom := &PendingRoom{RoomID: roomID, QueueTime: time.Now()}
		jsonData, err := json.Marshal(pendingRoom)
		if err != nil {
			return fmt.Errorf("failed to marshal %s to redis: %w", roomID, err)
		}
		err = rds.RPush(ctx, deleteQueueKey, jsonData).Err()
		if err != nil {
			return fmt.Errorf("failed to push %s to redis: %w", roomID, err)
		}
		promDeleteQueueGauge.Set(float64(rds.LLen(ctx, deleteQueueKey).Val()))
	} else {
		deleteQueue <- roomID
		promDeleteQueueGauge.Set(float64(len(deleteQueue)))
	}
	return nil
}

func loopLeaveQueue(ctx context.Context, wg *sync.WaitGroup) {
	defer func() {
		queueLog.Infoln("Queue leave loop exiting")
		wg.Done()
	}()
	for {
		success := consumeLeaveQueue(ctx)
		var wait time.Duration

		// we go as fast as we can unless there's nothing to do
		if !success {
			wait = time.Second * 1
		}

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
	}
}

func loopDeleteQueue(ctx context.Context, wg *sync.WaitGroup) {
	defer func() {
		queueLog.Infoln("Queue delete loop exiting")
		wg.Done()
	}()
	for {
		consumeDeleteQueue(ctx)
		select {
		case <-time.After(cfg.QueueSleep):
		case <-ctx.Done():
			return
		}
	}
}

func pushErrorQueue(roomID id.RoomID) {
	if rds == nil {
		return
	}
	queueLog.Debugln("Marking", roomID, "as errored in redis")
	ctx := context.Background()
	err := rds.RPush(ctx, errorQueueKey, roomID.String()).Err()
	if err != nil {
		queueLog.Errorln("Failed to mark %s as errored in redis: %v", roomID, err)
		return
	}
	promErrorQueueGauge.Set(float64(rds.LLen(ctx, errorQueueKey).Val()))
}

func popLeaveQueue(ctx context.Context) (*LeavingRoom, bool) {
	if rds != nil {
		nextItem, err := rds.BLPop(ctx, 0, leaveQueueKey).Result()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				queueLog.Errorln("Failed to get next leave item from redis:", err)
			}
			return nil, false
		}

		leavingRoom := &LeavingRoom{}
		if err := json.Unmarshal([]byte(nextItem[1]), leavingRoom); err != nil {
			queueLog.Errorln("Failed to unmarshal next leave item from redis:", err)
			return nil, false
		}

		promLeaveQueueGauge.Set(float64(rds.LLen(ctx, leaveQueueKey).Val()))

		return leavingRoom, true
	} else {
		select {
		case leavingRoom := <-leaveQueue:
			promLeaveQueueGauge.Set(float64(len(leaveQueue)))
			return leavingRoom, true
		case <-ctx.Done():
			promLeaveQueueGauge.Set(0)
			return nil, false
		}
	}
}

func consumeLeaveQueue(ctx context.Context) bool {
	leavingRoom, ok := popLeaveQueue(ctx)
	if !ok {
		return false
	}
	if cfg.DryRun {
		queueLog.Debugfln("Not requesting admin API to leave room %s (dry run)", leavingRoom.RoomID)
	} else {
		queueLog.Debugfln("Requesting admin API to leave room %s", leavingRoom.RoomID)
	}
	startTime := time.Now()
	adminContext := context.WithValue(ctx, logContextKey, queueLog)

	for _, userID := range leavingRoom.Kick {
		if userClient, err := AdminLogin(adminContext, userID); err != nil {
			queueLog.Warnfln("Failed to log in as %s to leave %s: %v", userID, leavingRoom.RoomID, err)
		} else if cfg.DryRun {
			queueLog.Debugfln("Not leaving %s as %s as we're in dry run mode", leavingRoom.RoomID, userID)
		} else if _, err = userClient.LeaveRoom(leavingRoom.RoomID); err != nil {
			queueLog.Warnfln("Failed to leave %s as %s: %w", leavingRoom.RoomID, userID, err)
		} else {
			queueLog.Debugfln("Successfully left %s as %s", leavingRoom.RoomID, userID)
		}
	}

	aliases, err := adminClient.GetAliases(leavingRoom.RoomID)
	if aliases != nil {
		for _, alias := range aliases.Aliases {
			if cfg.DryRun {
				queueLog.Debugfln("Not removing alias %s of %s as we're in dry run mode", alias, leavingRoom.RoomID)
			} else {
				if _, deleteErr := asmuxClient.DeleteAlias(alias); deleteErr != nil {
					queueLog.Warnfln("Failed to remove alias %s of %s: %v", alias, leavingRoom.RoomID, deleteErr)
				} else {
					queueLog.Debugfln("Successfully removed alias %s of %s", alias, leavingRoom.RoomID)
				}
			}
		}
	}

	if err == nil {
		err = PushDeleteQueue(context.Background(), leavingRoom.RoomID)
	}

	if err != nil {
		queueLog.Warnfln("Failed to push %s to delete queue: %w", leavingRoom.RoomID, err)

		if err = PushLeaveQueue(ctx, leavingRoom.RoomID, leavingRoom.Kick); err != nil {
			queueLog.Errorfln("Failed to put room %s back to leave queue: %v", leavingRoom.RoomID, err)
		}
		return false
	} else {
		leaveTime := time.Now().Sub(startTime)
		queueLog.Debugln("Room", leavingRoom.RoomID, "successfully left in", leaveTime, "and moved to delete queue")
		promLeaveCounter.Add(1)
		promLeaveSeconds.Observe(leaveTime.Seconds())
		return true
	}
}

func popDeleteQueue(ctx context.Context) (id.RoomID, bool) {
	if rds != nil {
		nextItem, err := rds.LRange(ctx, deleteQueueKey, 0, 0).Result()
		if err != nil {
			queueLog.Errorln("Failed to peek next item from redis:", err)
			return "", false
		}

		if len(nextItem) == 0 {
			return "", false
		}

		// we only check for due if we get valid json, otherwise it's a legacy plain room id
		pendingRoom := &PendingRoom{}
		if err := json.Unmarshal([]byte(nextItem[0]), pendingRoom); err == nil {
			if time.Since(pendingRoom.QueueTime) < cfg.PostponeDeletion {
				queueLog.Debugfln("Next item from delete queue is due on %v", pendingRoom.QueueTime.Add(cfg.PostponeDeletion))
				return "", false
			}
		}

		// really pop it now, we assume no one pushes to the head of the queue
		nextItem, err = rds.BLPop(ctx, 0, deleteQueueKey).Result()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				queueLog.Errorln("Failed to get next item from redis:", err)
			}
			return "", false
		}

		promDeleteQueueGauge.Set(float64(rds.LLen(ctx, deleteQueueKey).Val()))

		if err := json.Unmarshal([]byte(nextItem[1]), pendingRoom); err == nil {
			return pendingRoom.RoomID, true
		}

		return id.RoomID(nextItem[1]), true
	} else {
		select {
		case roomID := <-deleteQueue:
			promDeleteQueueGauge.Set(float64(len(deleteQueue)))
			return roomID, true
		case <-ctx.Done():
			promDeleteQueueGauge.Set(0)
			return "", false
		}
	}
}

func consumeDeleteQueue(ctx context.Context) {
	roomID, ok := popDeleteQueue(ctx)
	if !ok {
		return
	}
	if cfg.DryRun {
		queueLog.Debugfln("Not requesting admin API to clean up room %s (dry run)", roomID)
	} else {
		queueLog.Debugfln("Requesting admin API to clean up room %s", roomID)
	}
	startTime := time.Now()
	if len(cfg.AsmuxAccessToken) > 0 && cfg.AsmuxMainURL != nil {
		queueLog.Debugln("Requesting asmux to forget about room", roomID)
		err := asmuxDeleteRoom(ctx, roomID)
		if err != nil {
			queueLog.Warnfln("Failed to request asmux to forget about room %s: %v", roomID, err)
		}
	}
	_, err := adminDeleteRoom(ctx, ReqDeleteRoom{RoomID: roomID, Purge: true})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			queueLog.Debugfln("Context was canceled while cleaning up %s, putting it back in the queue", roomID)
			err = PushDeleteQueue(context.Background(), roomID)
			if err != nil {
				queueLog.Errorfln("Failed to put %s back in the queue: %v", roomID, err)
			}
		} else {
			queueLog.Warnfln("Failed to clean up %s: %v", roomID, err)
			go pushErrorQueue(roomID)
		}
	} else {
		deleteTime := time.Now().Sub(startTime)
		queueLog.Debugln("Room", roomID, "successfully cleaned up in", deleteTime)
		promDeleteCounter.Add(1)
		promDeleteSeconds.Observe(deleteTime.Seconds())
	}
}

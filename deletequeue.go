package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix/id"
)

// QueueSleep specifies how long to sleep between room deletions.
const QueueSleep = 1 * time.Minute

var queueLog = log.Sub("Queue")
var imq chan id.RoomID
var rds *redis.Client
const queueKey = "yeetserv:delete_queue"
const errorQueueKey = "yeetserv:error_queue"

func initQueue() {
	if len(cfg.RedisURL) > 0 {
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
	} else {
		imq = make(chan id.RoomID, 8192)
	}
}

func PushDeleteQueue(ctx context.Context, roomID id.RoomID) error {
	if rds != nil {
		err := rds.RPush(ctx, queueKey, roomID).Err()
		if err != nil {
			return fmt.Errorf("failed to push %s to redis: %w", roomID, err)
		}
	} else {
		imq <- roomID
	}
	return nil
}

func loopQueue(ctx context.Context, wg *sync.WaitGroup) {
	defer func() {
		queueLog.Infoln("Queue loop exiting")
		wg.Done()
	}()
	for {
		consumeQueue(ctx)
		select {
		case <-time.After(QueueSleep):
		case <-ctx.Done():
			return
		}
	}
}

func markAsErrored(roomID id.RoomID) {
	if rds == nil {
		return
	}
	queueLog.Debugln("Marking", roomID, "as errored in redis")
	err := rds.RPush(context.Background(), errorQueueKey, roomID).Err()
	if err != nil {
		queueLog.Errorln("Failed to mark %s as errored in redis: %v", roomID, err)
	}
}

func consumeQueue(ctx context.Context) {
	nextItem, err := rds.BLPop(ctx, 0, queueKey).Result()
	if err != nil {
		queueLog.Errorln("Failed to get next item from redis:", err)
		return
	}
	roomID := id.RoomID(nextItem[1])
	if cfg.DryRun {
		queueLog.Debugfln("Not requesting admin API to clean up room %s (dry run)", roomID)
	} else {
		queueLog.Debugfln("Requesting admin API to clean up room %s", roomID)
	}
	startTime := time.Now()
	_, err = adminDeleteRoom(ctx, ReqDeleteRoom{RoomID: roomID, Purge: true})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			queueLog.Debugfln("Context was canceled while cleaning up %s, putting it back in the queue", roomID)
			err = PushDeleteQueue(context.Background(), roomID)
			if err != nil {
				queueLog.Errorfln("Failed to put %s back in the queue: %v", roomID, err)
			}
		} else {
			queueLog.Warnfln("Failed to clean up %s: %v", roomID, err)
			go markAsErrored(roomID)
		}
	} else if err == nil {
		queueLog.Debugln("Room", roomID, "successfully cleaned up in", startTime.Sub(time.Now()))
	}
}

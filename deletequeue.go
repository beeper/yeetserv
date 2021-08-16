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

var queueLog = log.Sub("Queue")
var imq chan id.RoomID
var rds *redis.Client
const queueKey = "yeetserv:delete_queue"
const errorQueueKey = "yeetserv:error_queue"

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
	} else {
		imq = make(chan id.RoomID, 8192)
	}
}

func PushDeleteQueue(ctx context.Context, roomID id.RoomID) error {
	if rds != nil {
		err := rds.RPush(ctx, queueKey, roomID.String()).Err()
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
	err := rds.RPush(context.Background(), errorQueueKey, roomID.String()).Err()
	if err != nil {
		queueLog.Errorln("Failed to mark %s as errored in redis: %v", roomID, err)
	}
}

func popDeleteQueue(ctx context.Context) (id.RoomID, bool) {
	if rds != nil {
		nextItem, err := rds.BLPop(ctx, 0, queueKey).Result()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				queueLog.Errorln("Failed to get next item from redis:", err)
			}
			return "", false
		}
		return id.RoomID(nextItem[1]), true
	} else {
		select {
		case roomID := <-imq:
			return roomID, true
		case <-ctx.Done():
			return "", false
		}
	}
}

func consumeQueue(ctx context.Context) {
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
		queueLog.Debugln("Room", roomID, "successfully cleaned up in", time.Now().Sub(startTime))
	}
}

package main

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	log "maunium.net/go/maulogger/v2"
)

type Config struct {
	ListenAddress      string
	SynapseURL         string
	AsmuxURL           string
	AsmuxMainURL       *url.URL
	AsmuxDatabaseURL   string
	AsmuxAccessToken   string
	AdminAccessToken   string
	AdminUsername      string
	AdminPassword      string
	ThreadCount        int
	QueueSleep         time.Duration
	TrustForwardHeader bool
	DryRun             bool
	RedisURL           string
	PostponeDeletion   time.Duration
}

var cfg Config

func isTruthy(env string) bool {
	env = strings.ToLower(strings.TrimSpace(env))
	return env == "1" || env == "t" || env == "true" || env == "y" || env == "yes"
}

func readEnv() {
	cfg.ListenAddress = os.Getenv("LISTEN_ADDRESS")
	cfg.SynapseURL = os.Getenv("SYNAPSE_URL")
	cfg.AsmuxURL = os.Getenv("ASMUX_URL")
	cfg.AsmuxDatabaseURL = os.Getenv("ASMUX_DATABASE_URL")
	if len(cfg.AsmuxURL) == 0 {
		cfg.AsmuxURL = cfg.SynapseURL
	}
	if _, isSet := os.LookupEnv("ASMUX_MAIN_URL"); isSet {
		var err error
		cfg.AsmuxMainURL, err = url.Parse(os.Getenv("ASMUX_MAIN_URL"))
		if err != nil {
			log.Fatalln("Failed to parse asmux main URL:", err)
			os.Exit(2)
		}
	}
	cfg.AdminAccessToken = os.Getenv("ADMIN_ACCESS_TOKEN")
	cfg.AdminUsername = os.Getenv("ADMIN_USERNAME")
	cfg.AdminPassword = os.Getenv("ADMIN_PASSWORD")
	cfg.AsmuxAccessToken = os.Getenv("ASMUX_ACCESS_TOKEN")
	cfg.TrustForwardHeader = isTruthy(os.Getenv("TRUST_FORWARD_HEADERS"))
	cfg.DryRun = isTruthy(os.Getenv("DRY_RUN"))
	cfg.RedisURL = os.Getenv("REDIS_URL")
	if isTruthy(os.Getenv("DEBUG")) {
		log.DefaultLogger.PrintLevel = log.LevelDebug.Severity
	}
	log.DefaultLogger.TimeFormat = "Jan _2, 2006 15:04:05"
	queueSleepStr := os.Getenv("QUEUE_SLEEP")
	if len(queueSleepStr) == 0 {
		queueSleepStr = "60"
	}
	queueSleepInt, err := strconv.Atoi(queueSleepStr)
	if err != nil {
		log.Fatalln("QUEUE_SLEEP environment variable is not an integer")
		os.Exit(2)
	}
	cfg.QueueSleep = time.Duration(queueSleepInt) * time.Second
	if cfg.PostponeDeletion, err = time.ParseDuration(os.Getenv("POSTPONE_DELETION")); err != nil {
		cfg.PostponeDeletion = time.Second * 0
	}
	threadCountStr := os.Getenv("THREAD_COUNT")
	if len(threadCountStr) == 0 {
		threadCountStr = "5"
	}
	cfg.ThreadCount, err = strconv.Atoi(threadCountStr)
	if err != nil {
		log.Fatalln("THREAD_COUNT environment variable is not an integer")
	} else if len(cfg.ListenAddress) == 0 {
		log.Fatalln("LISTEN_ADDRESS environment variable is not set")
	} else if len(cfg.SynapseURL) == 0 {
		log.Fatalln("SYNAPSE_URL environment variable is not set")
	} else if len(cfg.AdminAccessToken) == 0 {
		if len(cfg.AdminUsername) == 0 && len(cfg.AdminPassword) == 0 {
			log.Fatalln("ADMIN_ACCESS_TOKEN environment variable is not set and ADMIN_USERNAME+ADMIN_PASSWORD is not set")
		} else {
			return
		}
	} else {
		return
	}
	os.Exit(2)
}

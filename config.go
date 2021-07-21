package main

import (
	"os"
	"strconv"
	"strings"

	log "maunium.net/go/maulogger/v2"
)

type Config struct {
	ListenAddress      string
	SynapseURL         string
	AsmuxURL           string
	AdminAccessToken   string
	ThreadCount        int
	TrustForwardHeader bool
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
	if len(cfg.AsmuxURL) == 0 {
		cfg.AsmuxURL = cfg.SynapseURL
	}
	cfg.AdminAccessToken = os.Getenv("ADMIN_ACCESS_TOKEN")
	cfg.TrustForwardHeader = isTruthy(os.Getenv("TRUST_FORWARD_HEADERS"))
	if isTruthy(os.Getenv("DEBUG")) {
		log.DefaultLogger.PrintLevel = log.LevelDebug.Severity
	}
	threadCountStr := os.Getenv("THREAD_COUNT")
	if len(threadCountStr) == 0 {
		threadCountStr = "5"
	}
	var err error
	cfg.ThreadCount, err = strconv.Atoi(threadCountStr)
	if err != nil {
		log.Fatalln("THREAD_COUNT environment variable is not an integer")
	} else if len(cfg.ListenAddress) == 0 {
		log.Fatalln("LISTEN_ADDRESS environment variable is not set")
	} else if len(cfg.SynapseURL) == 0 {
		log.Fatalln("SYNAPSE_URL environment variable is not set")
	} else if len(cfg.AdminAccessToken) == 0 {
		log.Fatalln("ADMIN_ACCESS_TOKEN environment variable is not set")
	} else {
		return
	}
	os.Exit(2)
}

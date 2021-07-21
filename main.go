package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix"
)

var adminClient *mautrix.Client

func makeAdminClient() {
	var err error
	adminClient, err = mautrix.NewClient(cfg.SynapseURL, "", cfg.AdminAccessToken)
	if err != nil {
		log.Fatalln("Failed to create admin client:", err)
		os.Exit(3)
	}
}

func main() {
	readEnv()
	makeAdminClient()

	router := mux.NewRouter()
	router.HandleFunc("/_matrix/client/unstable/com.beeper.yeetserv/clean_rooms", handleCleanRooms).Methods(http.MethodPost)
	router.Handle("/metrics", promhttp.Handler())
	server := &http.Server{
		Addr:    cfg.ListenAddress,
		Handler: router,
	}
	go func() {
		log.Infoln("Starting to listen on", cfg.ListenAddress)
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatalln("Error in listener:", err)
		}
	}()

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := server.Shutdown(ctx)
	if err != nil {
		log.Errorln("Failed to close server:", err)
	}
}

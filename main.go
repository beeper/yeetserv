package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
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
	if len(cfg.AdminAccessToken) == 0 {
		_, err = adminClient.Login(&mautrix.ReqLogin{
			Type: mautrix.AuthTypePassword,
			Identifier: mautrix.UserIdentifier{
				Type: mautrix.IdentifierTypeUser,
				User: cfg.AdminUsername,
			},
			Password:                 cfg.AdminPassword,
			DeviceID:                 "yeetserv",
			InitialDeviceDisplayName: "yeetserv",

			StoreCredentials: true,
		})
		if err != nil {
			log.Fatalln("Failed to obtain admin token:", err)
			os.Exit(5)
		}
		log.Infofln("Obtained an admin access token (for %s) token using provided credentials", adminClient.UserID)
	}
	// We use contexts for admin request timeout
	adminClient.Client.Timeout = 0
}

func main() {
	readEnv()
	makeAdminClient()
	initQueue()

	var wg sync.WaitGroup
	wg.Add(2)
	loopContext, stopLoop := context.WithCancel(context.Background())

	router := mux.NewRouter()
	router.HandleFunc("/_matrix/client/unstable/com.beeper.yeetserv/clean_all", handleCleanAllRooms).Methods(http.MethodPost)
	router.HandleFunc("/_matrix/client/unstable/com.beeper.yeetserv/queue", handleQueue).Methods(http.MethodPost)
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
		} else {
			log.Infoln("HTTP server closed")
		}
		wg.Done()
	}()
	go loopQueue(loopContext, &wg)

	if cfg.DryRun {
		log.Infoln("Running in dry run mode")
	} else {
		log.Infoln("Running in destructive mode")
	}

	log.Infofln("Rooms will wait in the delete queue for %v", cfg.PostponeDeletion)

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	ctrlCCounter := 0
	go func() {
		for {
			<-c
			ctrlCCounter++
			if ctrlCCounter >= 3 {
				log.Fatalln("Received", ctrlCCounter, "interrupts, force quitting...")
				os.Exit(10)
			}
		}
	}()

	stopLoop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := server.Shutdown(ctx)
	if err != nil {
		log.Errorln("Failed to close server:", err)
	}
	log.Infoln("Waiting for loop and server to exit")
	wg.Wait()
	log.Infoln("Everything shut down")
}

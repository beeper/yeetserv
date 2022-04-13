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

func makeAdminClientFromCredentials() {
	ghostClient, err := mautrix.NewClient(cfg.SynapseURL, "", "")
	if err != nil {
		log.Fatalln("Failed to create admin client:", err)
		os.Exit(6)
	}
	resp, err := ghostClient.Login(&mautrix.ReqLogin{
		Type: "m.login.password",
		Identifier: mautrix.UserIdentifier{
			Type: "m.id.user",
			User: cfg.AdminUsername.String(),
		},
		Password:                 cfg.AdminPassword,
		DeviceID:                 "yeetserv",
		InitialDeviceDisplayName: "Yeetserv",
	})
	if err != nil {
		log.Fatalln("Failed to obtain admin token")
		os.Exit(7)
	}
	adminClient, err = mautrix.NewClient(cfg.SynapseURL, "", resp.AccessToken)
	if err != nil {
		log.Fatalln("Failed to create admin client:", err)
		os.Exit(3)
	}
	log.Infoln("Obtained an admin access token using provided credentials")
	// We use contexts for admin request timeout
	adminClient.Client.Timeout = 0
}

func makeAdminClientFromAdminAccessToken() {
	var err error
	adminClient, err = mautrix.NewClient(cfg.SynapseURL, "", cfg.AdminAccessToken)
	if err != nil {
		log.Fatalln("Failed to create admin client:", err)
		os.Exit(3)
	}
	// We use contexts for admin request timeout
	adminClient.Client.Timeout = 0
}

func main() {
	readEnv()
	if len(cfg.AdminAccessToken) == 0 {
		makeAdminClientFromCredentials()
	} else {
		makeAdminClientFromAdminAccessToken()
	}
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

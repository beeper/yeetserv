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
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	log "maunium.net/go/maulogger/v2"

	"maunium.net/go/mautrix"
)

var adminClient *mautrix.Client
var asmuxClient *mautrix.Client
var asmuxDbPool *pgxpool.Pool

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

func makeAsmuxClient() {
	var err error
	asmuxClient, err = mautrix.NewClient(cfg.SynapseURL, "", cfg.AsmuxASToken)
	if err != nil {
		log.Fatalln("Failed to create asmux client:", err)
		os.Exit(3)
	}
}

func makeAsmuxDbPool() bool {
	if len(cfg.AsmuxDatabaseURL) > 0 {
		var err error
		asmuxDbPool, err = pgxpool.Connect(context.Background(), cfg.AsmuxDatabaseURL)
		if err != nil {
			log.Fatalln("Unable to connect to asmux database: %v", err)
			os.Exit(3)
		}
		return true
	}
	return false
}

func main() {
	readEnv()
	makeAdminClient()
	makeAsmuxClient()
	initQueue()

	didMakePool := makeAsmuxDbPool()
	if didMakePool {
		defer asmuxDbPool.Close()
	}

	var wg sync.WaitGroup
	wg.Add(3)
	loopContext, stopLoop := context.WithCancel(context.Background())

	router := mux.NewRouter()
	router.HandleFunc("/_matrix/client/unstable/com.beeper.yeetserv/clean_all", handleCleanAllRooms).Methods(http.MethodPost)
	router.HandleFunc("/_matrix/client/unstable/com.beeper.yeetserv/queue", handleQueue).Methods(http.MethodPost)
	router.HandleFunc("/_matrix/client/unstable/com.beeper.yeetserv/admin_clean_rooms", handleAdminCleanRooms).Methods(http.MethodPost)
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
	go loopLeaveQueue(loopContext, &wg)
	go loopDeleteQueue(loopContext, &wg)
	go loopQueueStats(loopContext, &wg)

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

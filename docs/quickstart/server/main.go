package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/models"
)

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func serve() error {
	worker, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	factory := func() gent.Model { return &DemoModel{Delay: 600 * time.Millisecond} }
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		name := os.Getenv("GENT_MODEL")
		if name == "" {
			return errors.New("set GENT_MODEL when using OPENAI_API_KEY")
		}
		factory = func() gent.Model { return models.NewOpenAI(key, name) }
	}
	codecs := gent.NewCodecRegistry()
	if err := newAgent(factory()).RegisterCodecs(codecs); err != nil {
		return err
	}
	store, err := openStore(env("RUN_DB", "runs.db"), codecs)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.db.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()
	api := &API{store: store, model: factory, worker: worker, running: map[string]*liveRun{}}
	listener, err := net.Listen("tcp", env("API_ADDR", "127.0.0.1:8080"))
	if err != nil {
		return err
	}
	server := &http.Server{Handler: api.routes(env("UI_DIR", "../client/dist")),
		ReadHeaderTimeout: 5 * time.Second}
	shutdown := make(chan struct{})
	go func() {
		defer close(shutdown)
		<-worker.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("HTTP shutdown: %v", err)
		}
	}()
	log.Printf("LISTEN http://%s", listener.Addr().String())
	err = server.Serve(listener)
	stop()
	<-shutdown
	api.wait.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func main() {
	if err := serve(); err != nil {
		log.Fatal(err)
	}
}

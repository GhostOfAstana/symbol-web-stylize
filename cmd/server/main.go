package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	assets "github.com/GhostOfAstana/symbol-web-stylize"
	"github.com/GhostOfAstana/symbol-web-stylize/internal/ai"
	"github.com/GhostOfAstana/symbol-web-stylize/internal/handlers"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	palettes, err := ai.NewClient(ai.ConfigFromEnv())
	if err != nil {
		return err
	}
	handler, err := handlers.New(assets.FS, palettes)
	if err != nil {
		return err
	}
	address := os.Getenv("ADDR")
	if address == "" {
		address = ":8080"
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	stopped, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		log.Printf("Сервер %s; помощник по цвету: %s", address, palettes.Mode())
		done <- server.ListenAndServe()
	}()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-stopped.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return err
		}
		return nil
	}
}

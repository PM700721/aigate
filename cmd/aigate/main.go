package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nguyenbahoanganh/aigate/internal/api"
	"github.com/nguyenbahoanganh/aigate/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	router, err := api.NewRouterWithProvider(cfg)
	if err != nil {
		log.Fatalf("failed to init provider: %v", err)
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		fmt.Printf("\n⚡ aigate v%s\n", config.Version)
		fmt.Printf("   ├─ Listening on http://%s:%d\n", cfg.Host, cfg.Port)
		fmt.Printf("   ├─ OpenAI API:    /v1/chat/completions\n")
		fmt.Printf("   ├─ Anthropic API: /v1/messages\n")
		fmt.Printf("   └─ Models:        /v1/models\n\n")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n⏳ Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
	fmt.Println("✅ Stopped.")
}

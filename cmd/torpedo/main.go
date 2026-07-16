package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"proton-torpedo/internal/logic"
	"proton-torpedo/internal/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	l := logic.New(logger)

	// Reconnect to the last active server if one was saved before this restart.
	if err := l.Restore(); err != nil {
		logger.Warn("failed to restore previous connection", "err", err)
	}

	srv := server.New(l)

	listen := os.Getenv("LISTEN_ADDR")
	if listen == "" {
		listen = ":8081"
	}

	fmt.Println("Starting server on", listen)
	if err := srv.ListenAndServer(listen); err != nil {
		if !errors.Is(err, http.ErrServerClosed) {
			fmt.Println("server error:", err)
		}

		return
	}
}

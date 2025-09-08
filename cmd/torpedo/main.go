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
	srv := server.New(l)

	fmt.Println("Starting server on :8080")
	if err := srv.ListenAndServer(":8080"); err != nil {
		if !errors.Is(err, http.ErrServerClosed) {
			fmt.Println("server error:", err)
		}

		return
	}
}

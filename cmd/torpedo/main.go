package main

import (
	"errors"
	"fmt"
	"net/http"
	"proton-torpedo/internal/logic"
	"proton-torpedo/internal/server"
)

func main() {
	l := logic.New()
	srv := server.New(l)

	fmt.Println("Starting server on :8080")
	if err := srv.ListenAndServer(":8080"); err != nil {
		if !errors.Is(err, http.ErrServerClosed) {
			fmt.Println("server error:", err)
		}

		return
	}
}

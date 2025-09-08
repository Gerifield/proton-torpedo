package server

import (
	"encoding/json"
	"net/http"

	"proton-torpedo/internal/logic"
)

// Logic .
type Logic struct {
	l *logic.Logic
}

// New .
func New(l *logic.Logic) *Logic {
	return &Logic{
		l: l,
	}
}

func (l *Logic) listServers(w http.ResponseWriter, r *http.Request) {
	servers, err := l.l.ServerList()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(servers)
}

func (l *Logic) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("./static/")))
	mux.HandleFunc("GET /api/list", l.listServers)

	return mux
}

func (l *Logic) ListenAndServer(addr string) error {
	return http.ListenAndServe(addr, l.routes())
}

package bookmarks

import (
	"encoding/json"
	"net/http"
	"time"
)

// Bookmark is one saved link owned by a user.
type Bookmark struct {
	ID        int64     `json:"id"`
	Owner     string    `json:"owner"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// Store lists a user's bookmarks.
type Store interface {
	List(owner string) ([]Bookmark, error)
}

// Server serves the bookmarks HTTP API.
type Server struct {
	Store Store
}

// ServeHTTP handles GET /bookmarks for the signed-in user.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	owner := r.Header.Get("X-User")
	if owner == "" {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	list, err := s.Store.List(owner)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

package access

import (
	"context"
	"encoding/json/v2"
	"net/http"
)

type Principal struct{ Owner string }
type principalKey struct{}
type Store interface {
	Delete(context.Context, string, string) error
}
type Handler struct{ Store Store }

func (h Handler) Delete(w http.ResponseWriter, r *http.Request) {
	p, ok := r.Context().Value(principalKey{}).(Principal)
	if !ok || p.Owner == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var request struct {
		Owner    string `json:"owner"`
		Resource string `json:"resource"`
	}
	if err := json.UnmarshalRead(r.Body, &request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := h.Store.Delete(r.Context(), request.Owner, request.Resource); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

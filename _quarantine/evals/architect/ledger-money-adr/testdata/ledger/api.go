package ledger

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// BalanceHandler serves GET /accounts/{id}/balance as
// {"account_id": 7, "balance": 12.34}. The mobile app and the partner
// reporting export read this JSON; both parse "balance" as a number.
func BalanceHandler(s *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "bad account id", http.StatusBadRequest)
			return
		}
		balance, err := s.Balance(r.Context(), id)
		if err != nil {
			http.Error(w, "balance unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"account_id": id, "balance": balance})
	}
}

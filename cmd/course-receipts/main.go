package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/example/edtech-receipt-service/internal/receipt"
)

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	sender := receipt.ReceiptSender{Email: receipt.NewClient(key)}

	http.HandleFunc("POST /orders/receipt", func(w http.ResponseWriter, r *http.Request) {
		var order receipt.Order
		if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		result, err := sender.Process(r.Context(), order)
		if err != nil {
			status := http.StatusBadGateway
			var apiErr *receipt.APIError
			if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
				status = apiErr.HTTPStatus
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	log.Println("course receipt service listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	sendafrica "github.com/sendafrica/sendafrica-go"
)

func main() {
	secret := os.Getenv("SENDAFRICA_WEBHOOK_SECRET")
	http.HandleFunc("/webhooks/sendafrica", func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "unable to read body", http.StatusBadRequest)
			return
		}
		event, err := sendafrica.ParseWebhook(payload, r.Header.Get("X-SendAfrica-Signature"), secret)
		if err != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		log.Printf("event=%s message_id=%s", event.Type, event.MessageID)
		w.WriteHeader(http.StatusNoContent)
	})
	fmt.Println("listening on http://localhost:8080/webhooks/sendafrica")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

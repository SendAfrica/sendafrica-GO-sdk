package main

import (
	"context"
	"fmt"
	"log"
	"os"

	sendafrica "github.com/sendafrica/sendafrica-go"
)

func main() {
	client := sendafrica.NewClient(os.Getenv("SENDAFRICA_API_KEY"))
	result, err := client.SendSMS(context.Background(), sendafrica.SendSMSRequest{
		To:      "0712345678",
		Message: "Hello from SendAfrica!",
	}, sendafrica.RequestOptions{IdempotencyKey: "example-hello-1"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("message=%s status=%s credits=%d\n", result.MessageID, result.Status, result.CreditsUsed)
}

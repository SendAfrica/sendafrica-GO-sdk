# SendAfrica Go SDK

An idiomatic, dependency-free Go client for the [SendAfrica REST API](https://docs.sendafrica.online/docs/api). The package is designed around the documented API envelope and provides typed responses, API-key authentication, request tracing, safe retries, idempotency keys, Tanzania phone normalization, SMS part calculation, and webhook signature verification.

## Status

This SDK covers the full documented developer surface: single SMS, bulk SMS, balance and credit history, message logs, mobile-money payments and vouchers, health and international rates, auth/accounts (registration, login, JWT refresh, OTP, OAuth, profile, API-key management), contact lists, campaigns, notifications, phone utilities, and webhook helpers. The HTTP core is stable; remaining documented areas (sender-ID management, packages/templates, and the agent chat API) can be added as follow-on resource groups.

For the full reference, see [`docs/SDK_GUIDE.md`](docs/SDK_GUIDE.md). It documents the public API, configuration options, request and response models, authentication, retries, idempotency, error handling, phone normalization, SMS encoding, webhook security, testing, production guidance, and extension points.

## Installation

```bash
go get github.com/sendafrica/sendafrica-go
```

The module currently targets Go 1.22 and uses only the standard library.

## Quickstart

```go
package main

import (
    "context"
    "fmt"
    "log"

    sendafrica "github.com/sendafrica/sendafrica-go"
)

func main() {
    client := sendafrica.NewClient("SA-your-api-key")

    result, err := client.SendSMS(context.Background(), sendafrica.SendSMSRequest{
        To:      "0712345678",
        Message: "Hello from SendAfrica!",
        Sender:  "MyBrand",
    }, sendafrica.RequestOptions{IdempotencyKey: "order-1234-confirmation"})
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.MessageID, result.Status, result.CreditsUsed)
}
```

If the API key argument is empty, the client reads `SENDAFRICA_API_KEY`. The default API base URL is `https://api.sendafrica.online/v1`; tests and private gateways can override it with `WithBaseURL`.

## Documentation

| Document | Description |
| --- | --- |
| [`docs/SDK_GUIDE.md`](docs/SDK_GUIDE.md) | Complete developer and API reference |
| [`examples/send-sms/main.go`](examples/send-sms/main.go) | Environment-based send example |
| [`examples/webhook/main.go`](examples/webhook/main.go) | Signature-verified webhook example |

## Supported operations

| Go method | HTTP route | Authentication | Purpose |
| --- | --- | --- | --- |
| `SendSMS` | `POST /sms/` | API key or bearer token | Send one SMS |
| `SendBulkSMS` | `POST /sms/bulk` | API key or bearer token | Send up to 100 recipients and inspect partial failures |
| `GetBalance` | `GET /credits/balance` | API key or bearer token | Read current credit balance |
| `GetCreditHistory` | `GET /credits/history` | API key or bearer token | Read the paginated credit ledger |
| `ListMessageLogs` | `GET /sms/logs` | API key or bearer token | Query paginated delivery history |
| `GetVoucherRate` | `GET /vouchers/rate` | Public | Read voucher tier pricing |
| `CreatePayment` | `POST /payments/` | API key or bearer token | Start a fixed-package mobile-money top-up |
| `CreateVoucher` | `POST /vouchers/` | API key or bearer token | Start a pay-as-you-go top-up |
| `Health` | `GET /health` | Public | Check API health |
| `GetRates` / `GetCountryRate` | `GET /rates` / `GET /rates/{country}` | Public | Read the international rate card |
| `Register` / `Login` / `Refresh` | `POST /auth/...` | Public | Account registration, login, JWT refresh rotation |
| `VerifyEmail` / `SendVerificationEmail` | `POST /auth/...` | Public | Email OTP verification |
| `ResetPassword` / `ResetPasswordConfirm` | `POST /auth/...` | Public | Password reset via OTP |
| `OAuthExchange` | `POST /auth/oauth/exchange` | Public | Redeem an exchange code for a JWT pair |
| `Me` / `UpdateMe` | `GET`/`PUT /auth/me` | Bearer token or API key | Read/update the current profile |
| `Logout` / `ChangePassword` | `POST /auth/...` | Bearer token or API key | End session / change password |
| `SendPhoneOTP` / `VerifyPhone` | `POST /auth/...` | Bearer token or API key | Phone verification via SMS |
| `ListAPIKeys` / `CreateAPIKey` / `DeleteAPIKey` | `GET`/`POST`/`DELETE /auth/api-keys` | Bearer token or API key | Manage developer API keys |
| `ListContactLists` / `CreateContactList` | `GET`/`POST /contact-lists/` | Bearer token or API key | Phonebook list CRUD |
| `ListContacts` / `AddContact` / `GetContact` / `UpdateContact` / `DeleteContact` | `/contact-lists/{id}/contacts` | Bearer token or API key | Contact CRUD with search |
| `AddContactPhone` / `DeleteContactPhone` | `/contact-lists/{id}/contacts/{cid}/phones` | Bearer token or API key | Attach/remove extra numbers |
| `ExportContacts` / `ImportContactsCSV` | `/contact-lists/{id}/...` | Bearer token or API key | CSV export and bulk import |
| `GetGoogleContactsStatus` / `GoogleContactsSync` / `GoogleContactsDisconnect` | `/contact-lists/google/...` | Bearer token or API key | One-way Google Contacts import |
| `ListCampaigns` / `CreateCampaign` / `GetCampaign` | `/campaigns/...` | Bearer token or API key | Schedule and track campaigns |
| `CancelCampaign` / `DeleteCampaign` | `/campaigns/{id}/...` | Bearer token or API key | Cancel/delete campaigns |
| `ListCampaignRecipients` | `GET /campaigns/{id}/recipients` | Bearer token or API key | Per-recipient delivery tracking |
| `ListNotifications` / `UnreadNotificationCount` | `GET /notifications/...` | Bearer token or API key | In-app notifications and badge |
| `MarkNotificationRead` / `MarkAllNotificationsRead` | `/notifications/...` | Bearer token or API key | Mark notifications read |
| `ParseWebhook` | local helper | Webhook secret | Verify HMAC-SHA256 and parse webhook payloads |

## Retries and idempotency

The client retries connection failures and HTTP 429/5xx responses up to three times by default. Backoff follows the SendAfrica documentation: 0.5, 1, and 2 seconds for the first three retry delays, capped at 8 seconds; a valid `Retry-After` header is honored when it is longer. Configure the retry count with `WithMaxRetries`.

For state-changing operations, pass a deterministic idempotency key in `RequestOptions`. For example, use `order-1234-confirmation` rather than a fresh random UUID so a retry from another process can replay the same SendAfrica result instead of sending twice.

## Errors

API failures are returned as `*sendafrica.APIError`, which includes the HTTP status, stable API error code, request ID, response body, headers, and parsed retry delay. Convenience methods include `IsRateLimited`, `IsInsufficientCredits`, and `IsUnauthorized`.

```go
result, err := client.SendSMS(ctx, sendafrica.SendSMSRequest{To: "0712345678", Message: "Hello"})
if err != nil {
    var apiErr *sendafrica.APIError
    if errors.As(err, &apiErr) && apiErr.IsInsufficientCredits() {
        // Ask the account owner to top up before retrying.
    }
}
_ = result
```

The client also returns `ErrMissingAPIKey` when a protected method is called without credentials and returns a wrapped `ErrInvalidPhone` for malformed Tanzania mobile numbers.

## Phone and SMS utilities

```go
phone, err := sendafrica.NormalizeTZPhone("+255 712 345 678")
valid := sendafrica.IsValidTZPhone(phone)
info := sendafrica.GetSMSPartInfo("Habari 😊 Bei yako ni 5000 TZS")
// phone: +255712345678
// valid: true
// info.Encoding: UCS-2; info.Parts: 1; info.CreditsRequired: 1
```

The utility accepts local `06xx`/`07xx`, `255...`, and `+255...` representations and normalizes valid Tanzania mobile numbers to E.164. It applies GSM-7 limits of 160 characters for one part and 153 for multipart messages; UCS-2 uses 70 and 67 respectively. GSM-7 extension characters count as two septets.

## Webhook verification

Verify the raw request body before decoding or re-serializing it. The helper accepts either the raw hex digest or the common `sha256=<digest>` form in `X-SendAfrica-Signature`.

```go
payload, _ := io.ReadAll(r.Body)
event, err := sendafrica.ParseWebhook(
    payload,
    r.Header.Get("X-SendAfrica-Signature"),
    os.Getenv("SENDAFRICA_WEBHOOK_SECRET"),
)
if err != nil {
    http.Error(w, "invalid signature", http.StatusUnauthorized)
    return
}
fmt.Println(event.Type, event.MessageID)
```

Gateway delivery reports with `status: "Success"` are normalized to `sms.delivered`; failed or undelivered reports become `sms.failed`. Event-style payloads that already contain a `type` field are preserved.

## Development

```bash
gofmt -w .
go test ./...
go vet ./...
```

The tests use `httptest.Server` and do not make network calls. They cover request serialization, phone normalization, retry behavior, API error classification, webhook HMAC verification, SMS part calculation, and every new resource group (health, rates, auth, contact lists, campaigns, and notifications).

## Documentation references

The implementation follows the [API overview](https://docs.sendafrica.online/docs/api), [authentication guide](https://docs.sendafrica.online/docs/authentication), [auth & accounts reference](https://docs.sendafrica.online/docs/api/auth), [SMS reference](https://docs.sendafrica.online/docs/api/sms), [errors and response format](https://docs.sendafrica.online/docs/errors), [rate limits and idempotency](https://docs.sendafrica.online/docs/rate-limits), [phone and SMS parts guide](https://docs.sendafrica.online/docs/phone-numbers), [credits reference](https://docs.sendafrica.online/docs/api/credits), [payments reference](https://docs.sendafrica.online/docs/api/payments), [contact lists reference](https://docs.sendafrica.online/docs/api/contacts), [campaigns reference](https://docs.sendafrica.online/docs/api/campaigns), [notifications reference](https://docs.sendafrica.online/docs/api/notifications), [SMS rates reference](https://docs.sendafrica.online/docs/api/rates), and [webhooks reference](https://docs.sendafrica.online/docs/api/webhooks).

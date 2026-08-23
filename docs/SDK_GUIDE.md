# SendAfrica Go SDK: Complete Developer Guide

## 1. Document purpose

This guide is the authoritative developer reference for the initial `sendafrica-go` client. It explains how the package maps Go types and methods to the SendAfrica REST API, how credentials and requests are handled, how retries and idempotency interact, how phone numbers and SMS parts are calculated, and how to verify webhook signatures safely.

The SDK is intentionally small, dependency-free, and based entirely on Go’s standard library. It is suitable for backend services, command-line programs, scheduled workers, HTTP handlers, and test suites that need to send or reconcile SMS through SendAfrica.

> **Important scope note:** The current implementation covers the highest-value API-key workflows: single SMS, bulk SMS, credits, message logs, payments, vouchers, phone utilities, and webhook verification. Contact lists, campaigns, notifications, registration, login, refresh-token rotation, and profile management are documented by SendAfrica but are not yet exposed as typed methods in this package.

## 2. Product and API context

SendAfrica exposes a versioned REST API under the `https://api.sendafrica.online/v1` base URL. The platform is primarily a Tanzania-focused SMS service, with API routes for sending messages, tracking message logs, managing credits, initiating mobile-money top-ups, and receiving delivery callbacks.[1] The API uses JSON request and response bodies except for CSV operations and provider callbacks.[2]

Every API response is described by a common envelope containing a success flag, a data object, an optional error object, optional metadata, a request identifier, and a timestamp. The high-level SDK methods decode the envelope’s `data` object into typed Go values. On failures, they return an `*APIError` that preserves the stable API error code, HTTP status, request ID, response body, headers, and retry information.

| API concept | SendAfrica behavior | SDK representation |
| --- | --- | --- |
| Versioned API | Routes are under `/v1` | `DefaultBaseURL` and `WithBaseURL` |
| Authentication | API key or bearer token | `NewClient` and `WithBearerToken` |
| Request tracing | `X-Request-Id` is sent on each request | Automatically generated UUID-like identifier |
| Idempotency | Supported on state-changing operations | `RequestOptions.IdempotencyKey` |
| Rate limits | Plan-based, communicated through headers | Automatic retry plus `APIError.RetryAfter` |
| JSON envelope | `success`, `data`, `error`, `meta`, `request_id`, `timestamp` | Typed response models and `APIError` |
| Tanzania phone handling | Local and international formats are normalized | `NormalizeTZPhone` and method-level normalization |
| Webhook signatures | HMAC-SHA256 over the raw body | `VerifyWebhookSignature` and `ParseWebhook` |

## 3. Installation and module layout

Install the package with:

```bash
go get github.com/sendafrica/sendafrica-go
```

The module targets Go 1.22 and has no third-party runtime dependencies. The repository has the following layout:

```text
sendafrica-go/
├── go.mod
├── sendafrica.go
├── phone.go
├── sendafrica_test.go
├── README.md
├── docs/
│   └── SDK_GUIDE.md
└── examples/
    ├── send-sms/
    │   └── main.go
    └── webhook/
        └── main.go
```

The main package is declared as `sendafrica`, even though the module import path contains the repository name. A conventional import alias is therefore optional:

```go
import sendafrica "github.com/sendafrica/sendafrica-go"
```

## 4. Creating and configuring a client

### 4.1 Basic construction

The primary constructor is:

```go
client := sendafrica.NewClient(apiKey, options...)
```

If `apiKey` is empty or contains only whitespace, the constructor reads `SENDAFRICA_API_KEY` from the process environment. This makes local development convenient while still allowing production applications to pass secrets from a secret manager.

```go
client := sendafrica.NewClient("")
```

The credential resolution order is therefore:

| Priority | Credential source | Behavior |
| --- | --- | --- |
| 1 | Explicit `apiKey` argument | Used when non-empty |
| 2 | `SENDAFRICA_API_KEY` | Used when the explicit argument is empty |
| 3 | No credential | Protected methods return `ErrMissingAPIKey` |

The raw API key should not be committed to source control, logged, placed in a URL, or embedded in a client-side application. SendAfrica documents that newly created keys are shown once and should be stored immediately in an environment or secrets manager.[3]

### 4.2 Client options

The constructor accepts functional options. Options are applied in the order supplied.

| Option | Effect | Typical use |
| --- | --- | --- |
| `WithBaseURL(rawURL)` | Replaces the default base URL after trimming a trailing slash | Tests, mock servers, private gateways |
| `WithHTTPClient(httpClient)` | Injects an `*http.Client` | Custom transport, proxy, observability, mTLS |
| `WithMaxRetries(n)` | Sets retry count for connection failures and retryable HTTP responses | Disable retries in tests or tune operational behavior |
| `WithBearerToken(token)` | Uses `Authorization: Bearer <token>` instead of `X-API-Key` | JWT-backed dashboard or internal flows |
| `WithUserAgent(agent)` | Replaces the default user agent | Service identification and telemetry |

Example configuration:

```go
httpClient := &http.Client{
    Timeout: 15 * time.Second,
}

client := sendafrica.NewClient(
    os.Getenv("SENDAFRICA_API_KEY"),
    sendafrica.WithHTTPClient(httpClient),
    sendafrica.WithMaxRetries(3),
    sendafrica.WithUserAgent("acme-orders/1.4.0"),
)
```

The default base URL is available as `sendafrica.DefaultBaseURL`. The current base URL can be inspected with `client.BaseURL()`.

### 4.3 Request context and timeouts

The SDK accepts a `context.Context` on every network method. The context controls cancellation and deadline behavior. The client itself does not impose a timeout on `http.DefaultClient`, so production services should inject an HTTP client with a timeout or use a context deadline.

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

balance, err := client.GetBalance(ctx)
```

When a context is canceled during retry backoff, the SDK stops waiting and returns the context error.

## 5. Authentication behavior

SendAfrica supports API keys for developer integrations and JWTs for dashboard/session flows.[3] The SDK sends an API key using the `X-API-Key` header. When `WithBearerToken` is configured, it sends an `Authorization: Bearer` header instead. The bearer token takes precedence over the API key if both are configured.

The client does not expose a login flow or refresh-token store. This is deliberate: access-token storage, refresh rotation, OAuth redirects, and user-session policy belong in the application layer. The SendAfrica documentation states that access tokens are short-lived and refresh tokens rotate on every use.[3]

Protected methods check for credentials locally before making a network request. If none are configured, they return:

```go
sendafrica.ErrMissingAPIKey
```

The name reflects the historical API-key workflow; the same error also applies when a bearer token is absent.

## 6. Sending a single SMS

### 6.1 Method and route

```go
func (c *Client) SendSMS(
    ctx context.Context,
    request SendSMSRequest,
    opts ...RequestOptions,
) (SMSResult, error)
```

This maps to:

```text
POST /v1/sms/
```

SendAfrica recommends API-key authentication for application SMS sending.[4] The SDK normalizes the `To` value locally before serializing the request. The request field named `Sender` maps to the REST field named `from`.

### 6.2 Request model

```go
type SendSMSRequest struct {
    To      string `json:"to"`
    Message string `json:"message"`
    Sender  string `json:"from,omitempty"`
}
```

| Field | Required | Description |
| --- | --- | --- |
| `To` | Yes | Tanzania mobile number in local or international format |
| `Message` | Yes | Message text; billing is based on SMS parts |
| `Sender` | No | Registered sender ID, serialized as `from` |

If `Sender` is omitted, SendAfrica uses its platform fallback sender. The documentation warns that an unregistered sender ID may not fail the request but can be replaced by the fallback sender at the carrier boundary.[4]

### 6.3 Result model

```go
type SMSResult struct {
    MessageID   string `json:"message_id"`
    Status      string `json:"status"`
    Cost        string `json:"cost"`
    CreditsUsed int    `json:"credits_used"`
}
```

A successful submit normally returns `Status == "sent"`. This means the message was accepted for gateway submission; it does not necessarily mean the handset has confirmed delivery. Later delivery state should be consumed from webhooks or reconciled through message logs.[5]

### 6.4 Basic example

```go
result, err := client.SendSMS(
    context.Background(),
    sendafrica.SendSMSRequest{
        To:      "0712345678",
        Message: "Your order is ready for collection.",
        Sender:  "MyBrand",
    },
)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("message=%s status=%s cost=%s credits=%d\n",
    result.MessageID,
    result.Status,
    result.Cost,
    result.CreditsUsed,
)
```

### 6.5 Safe retry example

Use a deterministic key tied to the business action, not a newly generated UUID on every retry:

```go
result, err := client.SendSMS(
    ctx,
    sendafrica.SendSMSRequest{
        To:      "0712345678",
        Message: "Payment received. Thank you.",
    },
    sendafrica.RequestOptions{
        IdempotencyKey: "payment-8472-receipt",
    },
)
```

SendAfrica documents successful idempotent responses as replayable for 24 hours. A duplicate request with the same key should replay the original response rather than charge or send again.[6]

## 7. Sending bulk SMS

### 7.1 Method and route

```go
func (c *Client) SendBulkSMS(
    ctx context.Context,
    request BulkSMSRequest,
    opts ...RequestOptions,
) (BulkSMSResult, error)
```

This maps to:

```text
POST /v1/sms/bulk
```

The API caps a bulk request at 100 recipients. SendAfrica intentionally returns HTTP 200 even when individual recipients fail, so callers must inspect the result counters and each result item rather than treating HTTP status alone as success.[4]

### 7.2 Request and result models

```go
type BulkSMSRequest struct {
    To      []string `json:"to"`
    Message string   `json:"message"`
    Sender  string   `json:"from,omitempty"`
}

type BulkSMSResult struct {
    Total   int           `json:"total"`
    Sent    int           `json:"sent"`
    Failed  int           `json:"failed"`
    Results []BulkSMSItem `json:"results"`
}

type BulkSMSItem struct {
    To          string `json:"to"`
    Status      string `json:"status"`
    MessageID   string `json:"message_id,omitempty"`
    CreditsUsed int    `json:"credits_used,omitempty"`
    Error       string `json:"error,omitempty"`
}
```

The client normalizes every recipient before sending. It rejects the whole request locally if any recipient cannot be normalized. The server remains responsible for enforcing the 100-recipient limit and for reporting provider-level or recipient-level failures.

```go
bulk, err := client.SendBulkSMS(
    ctx,
    sendafrica.BulkSMSRequest{
        To: []string{
            "0711111111",
            "+255722222222",
            "0754000111",
        },
        Message: "Flash sale today only.",
        Sender:  "MyBrand",
    },
    sendafrica.RequestOptions{
        IdempotencyKey: "campaign-flash-sale-2026-08-23",
    },
)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("total=%d sent=%d failed=%d\n", bulk.Total, bulk.Sent, bulk.Failed)
for _, item := range bulk.Results {
    if item.Status == "failed" {
        log.Printf("recipient %s failed: %s", item.To, item.Error)
    }
}
```

For audiences larger than 100, use a campaign workflow once campaign methods are added to the SDK, or partition work at the application level while preserving deterministic idempotency keys and respecting account rate limits.

## 8. Credits and billing

### 8.1 Read the balance

```go
func (c *Client) GetBalance(ctx context.Context) (CreditBalance, error)
```

Route:

```text
GET /v1/credits/balance
```

Model:

```go
type CreditBalance struct {
    AccountID string `json:"account_id"`
    Balance   int    `json:"balance"`
}
```

Example:

```go
balance, err := client.GetBalance(ctx)
if err != nil {
    return err
}
if balance.Balance < 100 {
    log.Printf("low balance: %d credits", balance.Balance)
}
```

The SendAfrica documentation recommends checking the balance before large batches, although a preflight check is advisory because other concurrent workers can consume credits between the check and the send.[7]

### 8.2 Read credit history

```go
func (c *Client) GetCreditHistory(
    ctx context.Context,
    query PageQuery,
) (CreditHistory, error)
```

Route:

```text
GET /v1/credits/history?page=1&per_page=25
```

Models:

```go
type PageQuery struct {
    Page    int
    PerPage int
}

type CreditTransaction struct {
    ID             string    `json:"id"`
    Type           string    `json:"type"`
    Status         string    `json:"status"`
    Amount         int       `json:"amount"`
    BalanceAfter   int       `json:"balance_after"`
    Description    string    `json:"description"`
    PaymentOrderID string    `json:"payment_order_id,omitempty"`
    CreatedAt      time.Time `json:"created_at"`
}

type CreditHistory struct {
    Items      []CreditTransaction `json:"items"`
    Total      int                 `json:"total"`
    Page       int                 `json:"page"`
    PerPage    int                 `json:"per_page"`
    TotalPages int                 `json:"total_pages"`
}
```

Zero-valued `Page` and `PerPage` fields are omitted from the query string, allowing the API defaults to apply. Positive values are encoded as decimal query parameters.

The ledger’s `Amount` is signed. A deduction is negative, while a purchase or refund is positive. The `Description` and `PaymentOrderID` fields are more informative than `Type` alone when distinguishing a top-up from a refund.[7]

## 9. Message logs and delivery reconciliation

### 9.1 Method and query model

```go
func (c *Client) ListMessageLogs(
    ctx context.Context,
    query MessageLogQuery,
) (MessageLogs, error)
```

Route:

```text
GET /v1/sms/logs
```

Query model:

```go
type MessageLogQuery struct {
    Page     int
    PerPage  int
    Status   string
    To       string
    FromDate string
    ToDate   string
}
```

Supported query fields map directly to the documented API filters:[5]

| Go field | Query parameter | Meaning |
| --- | --- | --- |
| `Page` | `page` | Page number |
| `PerPage` | `per_page` | Page size |
| `Status` | `status` | `sent`, `pending`, `delivered`, `failed`, or another API status |
| `To` | `to` | Recipient filter |
| `FromDate` | `from_date` | Start date filter |
| `ToDate` | `to_date` | End date filter |

### 9.2 Models

```go
type MessageLog struct {
    ID          string     `json:"id"`
    Recipient   string     `json:"recipient"`
    Message     string     `json:"message"`
    Sender      string     `json:"sender"`
    Status      string     `json:"status"`
    CreditsUsed int        `json:"credits_used"`
    CampaignID  *int       `json:"campaign_id"`
    CreatedAt   time.Time  `json:"created_at"`
    DeliveredAt *time.Time `json:"delivered_at"`
}

type MessageLogs struct {
    Items      []MessageLog `json:"items"`
    Total      int           `json:"total"`
    Page       int           `json:"page"`
    PerPage    int           `json:"per_page"`
    TotalPages int           `json:"total_pages"`
}
```

A `nil` `DeliveredAt` means the API did not provide a delivery timestamp. A `nil` `CampaignID` means the message was not associated with a campaign or that the field was null in the response.

### 9.3 Reconciliation pattern

Webhooks should be the primary delivery signal. Logs are useful as a periodic reconciliation source, especially for messages that remain in `sent` or `pending` longer than an operational threshold.

```go
logs, err := client.ListMessageLogs(ctx, sendafrica.MessageLogQuery{
    Page:    1,
    PerPage: 100,
    Status:  "sent",
})
if err != nil {
    return err
}

cutoff := time.Now().Add(-10 * time.Minute)
for _, message := range logs.Items {
    if message.CreatedAt.Before(cutoff) {
        log.Printf("message %s needs reconciliation", message.ID)
    }
}
```

Do not assume that `sent` means `delivered`. The documented lifecycle is asynchronous: a successful send may later become `pending`, `delivered`, or `failed`.[5]

## 10. Payments and vouchers

### 10.1 Fixed-package payments

```go
func (c *Client) CreatePayment(
    ctx context.Context,
    request CreatePaymentRequest,
) (Payment, error)
```

Route:

```text
POST /v1/payments/
```

Request model:

```go
type CreatePaymentRequest struct {
    PackageID int    `json:"package_id"`
    Provider  string `json:"provider"`
    Phone     string `json:"phone"`
}
```

Response model:

```go
type Payment struct {
    ID              string `json:"id"`
    Status          string `json:"status"`
    Source          string `json:"source"`
    Provider        string `json:"provider"`
    Amount          int    `json:"amount"`
    Currency        string `json:"currency"`
    CreditAmount    int    `json:"credit_amount"`
    CheckoutMessage string `json:"checkout_message"`
}
```

The SDK normalizes the supplied phone before submitting the request. The API may require the account phone to be verified and may return a provider-specific error when mobile-money requirements are not satisfied.[8]

```go
payment, err := client.CreatePayment(ctx, sendafrica.CreatePaymentRequest{
    PackageID: 2,
    Provider:  "snippe",
    Phone:     "0712345678",
})
if err != nil {
    return err
}
log.Printf("payment %s is %s; follow-up: %s", payment.ID, payment.Status, payment.CheckoutMessage)
```

### 10.2 Voucher rate and pay-as-you-go top-ups

The public voucher rate endpoint is available without credentials:

```go
func (c *Client) GetVoucherRate(ctx context.Context) (VoucherRate, error)
```

Route:

```text
GET /v1/vouchers/rate
```

Models:

```go
type VoucherTier struct {
    MaxAmountTZS     *int `json:"max_amount_tzs"`
    RateTZSPerCredit int  `json:"rate_tzs_per_credit"`
}

type VoucherRate struct {
    MinAmountTZS int           `json:"min_amount_tzs"`
    Tiers        []VoucherTier `json:"tiers"`
}
```

A `nil` `MaxAmountTZS` represents an open-ended upper tier. The rate card should be treated as authoritative for the current account pricing rather than hard-coded in application logic.

To create a voucher order:

```go
func (c *Client) CreateVoucher(
    ctx context.Context,
    request CreateVoucherRequest,
) (Payment, error)
```

```go
type CreateVoucherRequest struct {
    Amount   int    `json:"amount"`
    Provider string `json:"provider"`
    Phone    string `json:"phone"`
}
```

The API documentation describes voucher orders as mobile-money flows requiring a verified phone number.[8] The SDK starts the order and returns its status; it does not poll or block until payment confirmation.

## 11. Tanzania phone normalization

### 11.1 `NormalizeTZPhone`

```go
func NormalizeTZPhone(phone string) (string, error)
```

The function trims surrounding whitespace and removes spaces, hyphens, and parentheses. It accepts the following forms:

| Input form | Example | Output |
| --- | --- | --- |
| Local | `0712345678` | `+255712345678` |
| International with plus | `+255712345678` | `+255712345678` |
| International without plus | `255712345678` | `+255712345678` |
| Formatted international | `+255 712 345 678` | `+255712345678` |

The current implementation validates a Tanzania mobile number as `+255` followed by a `6` or `7` prefix and eight additional digits. Other country codes, malformed lengths, and non-mobile values return an error wrapping `ErrInvalidPhone`.

```go
normalized, err := sendafrica.NormalizeTZPhone("+255 (712) 345-678")
if err != nil {
    return err
}
fmt.Println(normalized) // +255712345678
```

Use `errors.Is` when callers need to classify the error:

```go
if errors.Is(err, sendafrica.ErrInvalidPhone) {
    // Ask the caller for a valid Tanzania mobile number.
}
```

### 11.2 `IsValidTZPhone`

```go
func IsValidTZPhone(phone string) bool
```

This is a convenience predicate around `NormalizeTZPhone`. It returns `true` only when normalization succeeds and does not expose the normalized value.

### 11.3 Method-level behavior

`SendSMS`, `SendBulkSMS`, `CreatePayment`, and `CreateVoucher` normalize phone inputs before making a network request. This prevents common formatting differences from reaching the API and avoids spending credits on a request that is locally known to be malformed.

Bulk sends are rejected before the request is sent if any element of `To` fails normalization. Applications that need partial acceptance should normalize each number independently before building `BulkSMSRequest`.

## 12. SMS encoding and part calculation

### 12.1 Encoding types

```go
type SMSEncoding string

const (
    EncodingGSM7 SMSEncoding = "GSM-7"
    EncodingUCS2 SMSEncoding = "UCS-2"
)
```

Use:

```go
func DetectEncoding(message string) SMSEncoding
```

The utility classifies a message as GSM-7 when every character belongs to the supported GSM-7 basic or extension alphabet. Characters outside that set, including emoji, Arabic, Chinese, smart punctuation, and many other Unicode characters, switch the complete message to UCS-2.[9]

### 12.2 Part information

```go
type SMSPartInfo struct {
    Encoding        SMSEncoding `json:"encoding"`
    Length          int         `json:"length"`
    Parts           int         `json:"parts"`
    CreditsRequired int         `json:"credits_required"`
}
```

Calculate it with:

```go
info := sendafrica.GetSMSPartInfo("Your order is ready.")
```

The documented limits are:

| Encoding | One-part limit | Multipart segment size | Credit rule |
| --- | ---: | ---: | --- |
| GSM-7 | 160 septets | 153 septets | One credit per part |
| UCS-2 | 70 characters | 67 characters | One credit per part |

GSM-7 extension characters such as `^`, `{`, `}`, `\\`, `[`, `]`, `~`, `|`, and `€` remain GSM-7 but consume two septets each. Therefore, `Length` for GSM-7 is a septet count, while `Length` for UCS-2 is a Unicode rune count.

```go
info := sendafrica.GetSMSPartInfo("Hello 😊")
fmt.Println(info.Encoding)        // UCS-2
fmt.Println(info.Parts)           // 1
fmt.Println(info.CreditsRequired) // 1
```

An empty message reports zero parts and zero required credits. The API will still validate whether an empty message is acceptable when a send method is called.

## 13. Webhook verification and parsing

### 13.1 Security model

SendAfrica documents HMAC-SHA256 signatures in the `X-SendAfrica-Signature` header. The signature must be computed over the exact raw request body, not over a decoded and re-serialized JSON object. Whitespace and key-order changes can invalidate a signature.[10]

The safe processing order is:

1. Read the raw body bytes.
2. Read the signature header.
3. Verify HMAC-SHA256 with the configured secret.
4. Only then parse the JSON event.
5. Return a fast 2xx response and move slow business processing to a queue or worker.

### 13.2 `VerifyWebhookSignature`

```go
func VerifyWebhookSignature(
    payload []byte,
    signature string,
    secret string,
) error
```

The helper accepts both a raw lowercase hexadecimal digest and the common prefixed form:

```text
f7a...9c2
sha256=f7a...9c2
```

It uses constant-time HMAC comparison. Failure is represented by `*WebhookSignatureError`, whose `Reason` explains whether the signature was missing, malformed, or mismatched.

```go
if err := sendafrica.VerifyWebhookSignature(body, signature, secret); err != nil {
    var signatureErr *sendafrica.WebhookSignatureError
    if errors.As(err, &signatureErr) {
        log.Printf("webhook rejected: %s", signatureErr.Reason)
    }
    http.Error(w, "invalid signature", http.StatusUnauthorized)
    return
}
```

### 13.3 `ParseWebhook`

```go
func ParseWebhook(
    payload []byte,
    signature string,
    secret string,
) (WebhookEvent, error)
```

Model:

```go
type WebhookEvent struct {
    Type      string         `json:"type"`
    MessageID string         `json:"message_id,omitempty"`
    Data      map[string]any `json:"data,omitempty"`
    Raw       json.RawMessage `json:"-"`
}
```

The parser first verifies the signature and then decodes the JSON object. It preserves the original payload in `Raw` and stores the complete decoded object in `Data`.

When the payload already contains a string `type`, that type is retained. For gateway-style delivery reports without a type, the helper infers:

| Payload status | Inferred event type |
| --- | --- |
| `Success` or `delivered` | `sms.delivered` |
| `failed` or `undelivered` | `sms.failed` |
| Any other or absent status | `sms.event` |

`MessageID` is read from `message_id`. If absent, an `id` beginning with `SA-` is used as a fallback. The full raw data map remains available because provider payloads may contain fields that are not yet modeled.

### 13.4 HTTP handler example

```go
func webhookHandler(secret string) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        body, err := io.ReadAll(r.Body)
        if err != nil {
            http.Error(w, "unable to read request", http.StatusBadRequest)
            return
        }

        event, err := sendafrica.ParseWebhook(
            body,
            r.Header.Get("X-SendAfrica-Signature"),
            secret,
        )
        if err != nil {
            http.Error(w, "invalid signature", http.StatusUnauthorized)
            return
        }

        switch event.Type {
        case "sms.delivered":
            // Enqueue a delivery update using event.MessageID.
        case "sms.failed":
            // Enqueue an operational alert or retry decision.
        }

        w.WriteHeader(http.StatusNoContent)
    })
}
```

Webhook handlers should be idempotent. SendAfrica documents deduplication for gateway callbacks, but your own database update or queue consumer should still tolerate duplicate deliveries.

## 14. Request options and headers

```go
type RequestOptions struct {
    IdempotencyKey string
    Headers        http.Header
}
```

`IdempotencyKey` is sent as `Idempotency-Key` when non-empty. `Headers` lets callers add request-specific headers. Header values are appended with `Header.Add`, so callers should not supply conflicting values for headers already managed by the client unless they understand the resulting wire format.

The client always sets:

| Header | Behavior |
| --- | --- |
| `Accept` | `application/json` |
| `Content-Type` | `application/json` when a request body exists |
| `X-Request-Id` | Fresh UUID-like identifier per attempt |
| `User-Agent` | `sendafrica-go/0.1.0` by default |
| `X-API-Key` | Sent when using API-key authentication |
| `Authorization` | Sent when a bearer token is configured |
| `Idempotency-Key` | Sent when supplied through request options |

A new `X-Request-Id` is generated for each retry attempt. This gives every wire request a distinct trace identifier, while the returned `APIError.RequestID` reports the identifier returned by the server when available.

## 15. Retry behavior

The SDK retries the following conditions:

| Condition | Retried by default? | Notes |
| --- | --- | --- |
| Connection or transport error | Yes | Retried until `maxRetries` is reached or context is canceled |
| HTTP 429 | Yes | Uses exponential backoff and honors `Retry-After` |
| HTTP 5xx | Yes | Uses exponential backoff |
| HTTP 4xx other than 429 | No | Returned as `*APIError` |
| HTTP 2xx with bulk item failures | No | Returned as a successful `BulkSMSResult`; inspect each item |

The default `maxRetries` is 3. The backoff delay is calculated as 0.5 seconds multiplied by powers of two, capped at 8 seconds. The SDK uses the server’s `Retry-After` delay when it is longer than the calculated delay.

```go
client := sendafrica.NewClient(
    os.Getenv("SENDAFRICA_API_KEY"),
    sendafrica.WithMaxRetries(5),
)
```

Retries are safe only when the operation is idempotent or the caller has supplied a deterministic idempotency key. A transport error does not prove that SendAfrica failed to receive or process a request. For SMS sends, always use an idempotency key when automatic retry is enabled in a business-critical path.[6]

## 16. Error handling

### 16.1 `APIError`

```go
type APIError struct {
    StatusCode int
    Code       string
    Message    string
    RequestID  string
    RetryAfter time.Duration
    Body       []byte
    Headers    http.Header
}
```

The SDK returns `*APIError` for non-2xx responses and for documented envelope failures represented with an error object. The original body and response headers are retained for diagnostics. The body may contain sensitive information in some environments; do not blindly log it in production.

```go
result, err := client.SendSMS(ctx, request)
if err != nil {
    var apiErr *sendafrica.APIError
    if errors.As(err, &apiErr) {
        log.Printf("SendAfrica request failed: code=%s status=%d request_id=%s",
            apiErr.Code,
            apiErr.StatusCode,
            apiErr.RequestID,
        )
    }
}
_ = result
```

### 16.2 Classification helpers

| Helper | Returns true when |
| --- | --- |
| `IsRateLimited()` | HTTP 429 or code `rate_limit_exceeded` |
| `IsInsufficientCredits()` | HTTP 402 or code `insufficient_credits` |
| `IsUnauthorized()` | HTTP 401 or codes `unauthorized` / `invalid_api_key` |

Example:

```go
_, err := client.SendSMS(ctx, request)
if err != nil {
    var apiErr *sendafrica.APIError
    if errors.As(err, &apiErr) {
        switch {
        case apiErr.IsInsufficientCredits():
            // Trigger top-up workflow or alert operations.
        case apiErr.IsRateLimited():
            // The SDK already retried; schedule a later attempt if needed.
        case apiErr.IsUnauthorized():
            // Rotate or repair the configured credential.
        }
    }
}
```

### 16.3 Common documented error codes

| HTTP | Code | Typical response |
| ---: | --- | --- |
| 400 | `validation_error` | Missing or invalid field |
| 400 | `invalid_phone` | Number is not a valid Tanzania mobile |
| 400 | `too_many_recipients` | Bulk request exceeds 100 recipients |
| 401 | `invalid_api_key` | API key is invalid |
| 401 | `missing_api_key` | Credential is missing |
| 402 | `insufficient_credits` | Account lacks enough credits |
| 403 | `account_inactive` | Account is suspended or inactive |
| 404 | `not_found` | Resource does not exist |
| 409 | `request_in_progress` | Same idempotency key is still processing |
| 429 | `rate_limit_exceeded` | Account or IP exceeded its rate limit |
| 500 | `server_error` | Internal platform error |

The stable `Code` string is the preferred value for application branching. Error messages may change wording.

## 17. API key fingerprinting

The package exposes:

```go
func APIKeyHash(key string) string
```

This computes a SHA-256 hexadecimal fingerprint locally. It does not call SendAfrica and cannot recover or validate a key. It can be used to compare whether two configured secrets are identical without logging the raw value.

```go
fingerprint := sendafrica.APIKeyHash(os.Getenv("SENDAFRICA_API_KEY"))
log.Printf("configured key fingerprint: %s", fingerprint)
```

Even a hash can be sensitive in some threat models. Treat it as an operational identifier, not as a replacement for secret handling.

## 18. Testing and local development

The repository tests use `httptest.Server`, so they do not send real SMS, create payments, or call the production API. Run the complete verification suite with:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
git diff --check
```

The tests cover:

| Area | Coverage |
| --- | --- |
| Phone utilities | Local, international, formatted, and invalid Tanzania numbers |
| SMS calculator | GSM-7, extension characters, UCS-2, and multipart thresholds |
| HTTP serialization | Normalized request body and JSON decoding |
| Headers | Request tracing and idempotency key propagation |
| Retry behavior | Repeated 5xx responses and eventual success |
| Error classification | Missing credentials and insufficient-credit classification |
| Webhooks | HMAC verification, `sha256=` prefix handling, and event inference |

A custom server can be used to test an integration without changing application code:

```go
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // Assert method, URL, headers, and body.
}))
defer server.Close()

client := sendafrica.NewClient(
    "SA-test",
    sendafrica.WithBaseURL(server.URL+"/v1"),
    sendafrica.WithMaxRetries(0),
)
```

## 19. Production operating guidance

Use a dedicated API key per environment or service where SendAfrica’s account controls permit it. Keep the key in a secret manager, rotate it without committing it to the repository, and avoid printing request headers in logs. Set an HTTP timeout and propagate request contexts from the surrounding job, HTTP request, or queue message.

For SMS sends, choose deterministic idempotency keys based on the business event. Store the SendAfrica message ID with the business record so later webhook events and reconciliation queries can be correlated. Treat `sent` as gateway acceptance rather than handset delivery. Use webhooks for near-real-time updates and message logs for scheduled reconciliation.

For bulk sends, inspect `Sent`, `Failed`, and every `BulkSMSItem`. Do not retry the entire batch blindly after a partial failure; doing so may duplicate recipients that already succeeded. Instead, record successful message IDs and retry only failed recipients with a new business-attempt key or an idempotency strategy appropriate to your workflow.

For payments, treat `pending` as an intermediate state. The SDK initiates the mobile-money order but does not wait for the user to approve a USSD prompt. Payment confirmation should be handled by the account’s normal notification or polling workflow once those resource methods are added.

For webhooks, verify signatures over the raw body, make consumers idempotent, respond quickly, and move slow work to a background queue. Do not trust the event type, message ID, or provider payload until signature verification succeeds.

## 20. Current limitations and extension roadmap

The client’s high-level methods return typed `data` values and errors. The current public API does not expose a separate result wrapper carrying successful-response metadata such as `request_id`, response timestamp, rate-limit headers, or the `Idempotent-Replay` header. Applications that require those values should use a future response-wrapper extension rather than relying on undocumented internals.

The next resource groups can be added without changing the underlying HTTP design:

| Planned area | Documented routes |
| --- | --- |
| Auth and accounts | `/auth/register`, `/auth/login`, `/auth/refresh`, `/auth/me`, API-key management |
| Contact lists | `/contact-lists/...` |
| Campaigns | `/campaigns/...` |
| Notifications | `/notifications/...` |
| Sender IDs | Sender-ID management routes from the API reference |
| Rates and packages | `/rates`, `/packages`, `/templates` |
| Agent API | `/agent/chat` |

When extending the SDK, keep the existing conventions: accept `context.Context`, use typed request and response structs, preserve nullable API values with pointers where appropriate, normalize phone numbers before network calls, expose idempotency options on mutating methods, and return `*APIError` for server failures.

## 21. Complete reference: exported API

The following table summarizes the current exported surface.

| Export | Kind | Purpose |
| --- | --- | --- |
| `DefaultBaseURL` | Constant | Default API base URL |
| `ErrMissingAPIKey` | Error | Missing credential for a protected method |
| `ErrInvalidPhone` | Error | Invalid Tanzania mobile number |
| `Client` | Type | Configured API client |
| `Option` | Type | Functional constructor option |
| `NewClient` | Function | Construct a client |
| `WithBaseURL` | Function | Override API base URL |
| `WithHTTPClient` | Function | Inject HTTP transport/client |
| `WithMaxRetries` | Function | Set retry count |
| `WithBearerToken` | Function | Configure bearer-token authentication |
| `WithUserAgent` | Function | Set custom user agent |
| `RequestOptions` | Type | Per-request idempotency and headers |
| `APIResponse` | Type | Common response envelope model |
| `APIErrorBody` | Type | Error object in the API envelope |
| `APIError` | Type | Structured request/API failure |
| `SendSMSRequest` | Type | Single SMS request |
| `SMSResult` | Type | Single SMS result |
| `SendSMS` | Method | Send one SMS |
| `BulkSMSRequest` | Type | Bulk SMS request |
| `BulkSMSResult` | Type | Bulk summary and item results |
| `BulkSMSItem` | Type | Per-recipient bulk result |
| `SendBulkSMS` | Method | Send up to 100 recipients |
| `CreditBalance` | Type | Account credit balance |
| `GetBalance` | Method | Fetch current credit balance |
| `PageQuery` | Type | Common page/per-page query |
| `CreditTransaction` | Type | Credit ledger item |
| `CreditHistory` | Type | Paginated credit history |
| `GetCreditHistory` | Method | Fetch credit ledger |
| `MessageLogQuery` | Type | Message-log filters |
| `MessageLog` | Type | Individual message log |
| `MessageLogs` | Type | Paginated message logs |
| `ListMessageLogs` | Method | Fetch message logs |
| `CreatePaymentRequest` | Type | Fixed-package payment request |
| `Payment` | Type | Payment or voucher order |
| `CreatePayment` | Method | Start fixed-package top-up |
| `VoucherTier` | Type | Voucher pricing tier |
| `VoucherRate` | Type | Voucher pricing response |
| `GetVoucherRate` | Method | Fetch public voucher rates |
| `CreateVoucherRequest` | Type | Pay-as-you-go top-up request |
| `CreateVoucher` | Method | Start voucher order |
| `SMSEncoding` | Type | GSM-7 or UCS-2 encoding |
| `EncodingGSM7` | Constant | GSM-7 encoding identifier |
| `EncodingUCS2` | Constant | UCS-2 encoding identifier |
| `SMSPartInfo` | Type | Encoding, length, parts, and credits |
| `NormalizeTZPhone` | Function | Normalize a Tanzania mobile number |
| `IsValidTZPhone` | Function | Validate a Tanzania mobile number |
| `DetectEncoding` | Function | Detect SMS encoding |
| `GetSMSPartInfo` | Function | Calculate SMS parts and credits |
| `WebhookEvent` | Type | Parsed webhook event |
| `WebhookSignatureError` | Type | Webhook verification failure |
| `VerifyWebhookSignature` | Function | Verify HMAC-SHA256 signature |
| `ParseWebhook` | Function | Verify and decode webhook payload |
| `APIKeyHash` | Function | Compute local SHA-256 key fingerprint |

## References

[1]: https://docs.sendafrica.online/docs/introduction "SendAfrica Introduction"
[2]: https://docs.sendafrica.online/docs/api "SendAfrica API Overview"
[3]: https://docs.sendafrica.online/docs/authentication "SendAfrica Authentication"
[4]: https://docs.sendafrica.online/docs/api/sms "SendAfrica Send SMS API"
[5]: https://docs.sendafrica.online/docs/api/message-logs "SendAfrica Message Logs"
[6]: https://docs.sendafrica.online/docs/rate-limits "SendAfrica Rate Limits and Idempotency"
[7]: https://docs.sendafrica.online/docs/api/credits "SendAfrica Credits and Billing"
[8]: https://docs.sendafrica.online/docs/api/payments "SendAfrica Payments and Vouchers"
[9]: https://docs.sendafrica.online/docs/phone-numbers "SendAfrica Phone Numbers and SMS Parts"
[10]: https://docs.sendafrica.online/docs/api/webhooks "SendAfrica Webhooks"

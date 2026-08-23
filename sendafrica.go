// Package sendafrica provides an idiomatic Go client for the SendAfrica REST API.
package sendafrica

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.sendafrica.online/v1"

var (
	ErrMissingAPIKey = errors.New("sendafrica: API key or bearer token is required")
	ErrInvalidPhone  = errors.New("sendafrica: invalid Tanzania mobile number")
)

type Client struct {
	baseURL    string
	apiKey     string
	bearer     string
	httpClient *http.Client
	maxRetries int
	userAgent  string
}

type Option func(*Client)

func WithBaseURL(rawURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(rawURL, "/") }
}
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
		}
	}
}
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}
func WithBearerToken(token string) Option {
	return func(c *Client) { c.bearer = strings.TrimSpace(token) }
}
func WithUserAgent(agent string) Option {
	return func(c *Client) { c.userAgent = strings.TrimSpace(agent) }
}

func NewClient(apiKey string, opts ...Option) *Client {
	if strings.TrimSpace(apiKey) == "" {
		apiKey = os.Getenv("SENDAFRICA_API_KEY")
	}
	c := &Client{
		baseURL:    DefaultBaseURL,
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: http.DefaultClient,
		maxRetries: 3,
		userAgent:  "sendafrica-go/0.1.0",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) BaseURL() string { return c.baseURL }

type RequestOptions struct {
	IdempotencyKey string
	Headers        http.Header
}

type APIResponse struct {
	Success   bool            `json:"success"`
	Error     *APIErrorBody   `json:"error"`
	Meta      json.RawMessage `json:"meta"`
	RequestID string          `json:"request_id"`
	Timestamp time.Time       `json:"timestamp"`
}

type APIErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
	RetryAfter time.Duration
	Body       []byte
	Headers    http.Header
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("sendafrica: HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("sendafrica: %s (%s, HTTP %d)", e.Message, e.Code, e.StatusCode)
}
func (e *APIError) IsRateLimited() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.Code == "rate_limit_exceeded"
}
func (e *APIError) IsInsufficientCredits() bool {
	return e.StatusCode == http.StatusPaymentRequired || e.Code == "insufficient_credits"
}
func (e *APIError) IsUnauthorized() bool {
	return e.StatusCode == http.StatusUnauthorized || e.Code == "unauthorized" || e.Code == "invalid_api_key"
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any, ro RequestOptions) (*APIResponse, error) {
	endpoint := strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("sendafrica: marshal request: %w", err)
		}
	}

	for attempt := 0; ; attempt++ {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
		if err != nil {
			return nil, fmt.Errorf("sendafrica: create request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("X-Request-Id", newRequestID())
		if c.userAgent != "" {
			req.Header.Set("User-Agent", c.userAgent)
		}
		if c.bearer != "" {
			req.Header.Set("Authorization", "Bearer "+c.bearer)
		} else {
			req.Header.Set("X-API-Key", c.apiKey)
		}
		if ro.IdempotencyKey != "" {
			req.Header.Set("Idempotency-Key", ro.IdempotencyKey)
		}
		for key, values := range ro.Headers {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt < c.maxRetries && ctx.Err() == nil {
				if err := sleepBackoff(ctx, attempt, 0); err != nil {
					return nil, err
				}
				continue
			}
			return nil, fmt.Errorf("sendafrica: request failed: %w", err)
		}
		responseBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("sendafrica: read response: %w", readErr)
		}

		var envelope struct {
			APIResponse
			Data json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(responseBody, &envelope)
		apiErr := &APIError{StatusCode: resp.StatusCode, RequestID: envelope.RequestID, Body: responseBody, Headers: resp.Header.Clone()}
		if envelope.Error != nil {
			apiErr.Code, apiErr.Message = envelope.Error.Code, envelope.Error.Message
		}
		if apiErr.Message == "" {
			apiErr.Message = http.StatusText(resp.StatusCode)
		}
		apiErr.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))

		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt < c.maxRetries {
			if err := sleepBackoff(ctx, attempt, apiErr.RetryAfter); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 || (envelope.Success == false && envelope.Error != nil) {
			return &envelope.APIResponse, apiErr
		}
		if out != nil && len(envelope.Data) > 0 && string(envelope.Data) != "null" {
			if err := json.Unmarshal(envelope.Data, out); err != nil {
				return &envelope.APIResponse, fmt.Errorf("sendafrica: decode response: %w", err)
			}
		}
		return &envelope.APIResponse, nil
	}
}

func sleepBackoff(ctx context.Context, attempt int, retryAfter time.Duration) error {
	delay := 500 * time.Millisecond * time.Duration(1<<attempt)
	if delay > 8*time.Second {
		delay = 8 * time.Second
	}
	if retryAfter > delay {
		delay = retryAfter
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseRetryAfter(raw string) time.Duration {
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("sendafrica-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(b[:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:]))
}

// Send SMS models and methods.
type SendSMSRequest struct {
	To      string `json:"to"`
	Message string `json:"message"`
	Sender  string `json:"from,omitempty"`
}
type SMSResult struct {
	MessageID   string `json:"message_id"`
	Status      string `json:"status"`
	Cost        string `json:"cost"`
	CreditsUsed int    `json:"credits_used"`
}

func (c *Client) SendSMS(ctx context.Context, request SendSMSRequest, opts ...RequestOptions) (SMSResult, error) {
	if err := c.requireAuth(); err != nil {
		return SMSResult{}, err
	}
	var err error
	request.To, err = NormalizeTZPhone(request.To)
	if err != nil {
		return SMSResult{}, err
	}
	var out SMSResult
	_, err = c.do(ctx, http.MethodPost, "/sms/", nil, request, &out, firstRequestOptions(opts))
	return out, err
}

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

func (c *Client) SendBulkSMS(ctx context.Context, request BulkSMSRequest, opts ...RequestOptions) (BulkSMSResult, error) {
	if err := c.requireAuth(); err != nil {
		return BulkSMSResult{}, err
	}
	for i, phone := range request.To {
		normalized, err := NormalizeTZPhone(phone)
		if err != nil {
			return BulkSMSResult{}, err
		}
		request.To[i] = normalized
	}
	var out BulkSMSResult
	_, err := c.do(ctx, http.MethodPost, "/sms/bulk", nil, request, &out, firstRequestOptions(opts))
	return out, err
}

// Credits and logs.
type CreditBalance struct {
	AccountID string `json:"account_id"`
	Balance   int    `json:"balance"`
}

func (c *Client) GetBalance(ctx context.Context) (CreditBalance, error) {
	if err := c.requireAuth(); err != nil {
		return CreditBalance{}, err
	}
	var out CreditBalance
	_, err := c.do(ctx, http.MethodGet, "/credits/balance", nil, nil, &out, RequestOptions{})
	return out, err
}

type PageQuery struct {
	Page    int
	PerPage int
}

func (q PageQuery) values() url.Values {
	v := url.Values{}
	if q.Page > 0 {
		v.Set("page", strconv.Itoa(q.Page))
	}
	if q.PerPage > 0 {
		v.Set("per_page", strconv.Itoa(q.PerPage))
	}
	return v
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

func (c *Client) GetCreditHistory(ctx context.Context, q PageQuery) (CreditHistory, error) {
	if err := c.requireAuth(); err != nil {
		return CreditHistory{}, err
	}
	var out CreditHistory
	_, err := c.do(ctx, http.MethodGet, "/credits/history", q.values(), nil, &out, RequestOptions{})
	return out, err
}

type MessageLogQuery struct {
	Page     int
	PerPage  int
	Status   string
	To       string
	FromDate string
	ToDate   string
}

func (q MessageLogQuery) values() url.Values {
	v := (PageQuery{Page: q.Page, PerPage: q.PerPage}).values()
	if q.Status != "" {
		v.Set("status", q.Status)
	}
	if q.To != "" {
		v.Set("to", q.To)
	}
	if q.FromDate != "" {
		v.Set("from_date", q.FromDate)
	}
	if q.ToDate != "" {
		v.Set("to_date", q.ToDate)
	}
	return v
}

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
	Total      int          `json:"total"`
	Page       int          `json:"page"`
	PerPage    int          `json:"per_page"`
	TotalPages int          `json:"total_pages"`
}

func (c *Client) ListMessageLogs(ctx context.Context, q MessageLogQuery) (MessageLogs, error) {
	if err := c.requireAuth(); err != nil {
		return MessageLogs{}, err
	}
	var out MessageLogs
	_, err := c.do(ctx, http.MethodGet, "/sms/logs", q.values(), nil, &out, RequestOptions{})
	return out, err
}

// Payments and vouchers.
type CreatePaymentRequest struct {
	PackageID int    `json:"package_id"`
	Provider  string `json:"provider"`
	Phone     string `json:"phone"`
}
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

func (c *Client) CreatePayment(ctx context.Context, request CreatePaymentRequest) (Payment, error) {
	if err := c.requireAuth(); err != nil {
		return Payment{}, err
	}
	var err error
	request.Phone, err = NormalizeTZPhone(request.Phone)
	if err != nil {
		return Payment{}, err
	}
	var out Payment
	_, err = c.do(ctx, http.MethodPost, "/payments/", nil, request, &out, RequestOptions{})
	return out, err
}

type VoucherTier struct {
	MaxAmountTZS     *int `json:"max_amount_tzs"`
	RateTZSPerCredit int  `json:"rate_tzs_per_credit"`
}
type VoucherRate struct {
	MinAmountTZS int           `json:"min_amount_tzs"`
	Tiers        []VoucherTier `json:"tiers"`
}

func (c *Client) GetVoucherRate(ctx context.Context) (VoucherRate, error) {
	var out VoucherRate
	_, err := c.do(ctx, http.MethodGet, "/vouchers/rate", nil, nil, &out, RequestOptions{})
	return out, err
}

type CreateVoucherRequest struct {
	Amount   int    `json:"amount"`
	Provider string `json:"provider"`
	Phone    string `json:"phone"`
}

func (c *Client) CreateVoucher(ctx context.Context, request CreateVoucherRequest) (Payment, error) {
	if err := c.requireAuth(); err != nil {
		return Payment{}, err
	}
	var err error
	request.Phone, err = NormalizeTZPhone(request.Phone)
	if err != nil {
		return Payment{}, err
	}
	var out Payment
	_, err = c.do(ctx, http.MethodPost, "/vouchers/", nil, request, &out, RequestOptions{})
	return out, err
}

func firstRequestOptions(opts []RequestOptions) RequestOptions {
	if len(opts) > 0 {
		return opts[0]
	}
	return RequestOptions{}
}
func (c *Client) requireAuth() error {
	if c.apiKey == "" && c.bearer == "" {
		return ErrMissingAPIKey
	}
	return nil
}

// Webhook helpers.
type WebhookEvent struct {
	Type      string          `json:"type"`
	MessageID string          `json:"message_id,omitempty"`
	Data      map[string]any  `json:"data,omitempty"`
	Raw       json.RawMessage `json:"-"`
}
type WebhookSignatureError struct{ Reason string }

func (e *WebhookSignatureError) Error() string {
	return "sendafrica: webhook signature verification failed: " + e.Reason
}
func VerifyWebhookSignature(payload []byte, signature, secret string) error {
	if signature == "" {
		return &WebhookSignatureError{"missing signature"}
	}
	provided := strings.TrimSpace(signature)
	if strings.HasPrefix(provided, "sha256=") {
		provided = strings.TrimPrefix(provided, "sha256=")
	}
	providedBytes, err := hex.DecodeString(provided)
	if err != nil {
		return &WebhookSignatureError{"signature is not valid hex"}
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	if !hmac.Equal(providedBytes, mac.Sum(nil)) {
		return &WebhookSignatureError{"signature mismatch"}
	}
	return nil
}
func ParseWebhook(payload []byte, signature, secret string) (WebhookEvent, error) {
	if err := VerifyWebhookSignature(payload, signature, secret); err != nil {
		return WebhookEvent{}, err
	}
	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		return WebhookEvent{}, fmt.Errorf("sendafrica: decode webhook: %w", err)
	}
	event := WebhookEvent{Raw: append(json.RawMessage(nil), payload...), Data: data}
	if typeValue, ok := data["type"].(string); ok {
		event.Type = typeValue
	}
	if id, ok := data["message_id"].(string); ok {
		event.MessageID = id
	}
	if event.MessageID == "" {
		if id, ok := data["id"].(string); ok && strings.HasPrefix(id, "SA-") {
			event.MessageID = id
		}
	}
	if event.Type == "" {
		status, _ := data["status"].(string)
		switch strings.ToLower(status) {
		case "success", "delivered":
			event.Type = "sms.delivered"
		case "failed", "undelivered":
			event.Type = "sms.failed"
		default:
			event.Type = "sms.event"
		}
	}
	return event, nil
}

func normalizeOrOriginal(phone string) string {
	normalized, err := NormalizeTZPhone(phone)
	if err != nil {
		return phone
	}
	return normalized
}

// APIKeyHash is useful for local secret fingerprinting without storing the raw key.
func APIKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

package sendafrica

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeTZPhone(t *testing.T) {
	cases := map[string]string{
		"0712345678":       "+255712345678",
		"+255 712 345 678": "+255712345678",
		"255712345678":     "+255712345678",
	}
	for input, want := range cases {
		got, err := NormalizeTZPhone(input)
		if err != nil || got != want {
			t.Fatalf("NormalizeTZPhone(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if IsValidTZPhone("+254712345678") {
		t.Fatal("expected a non-Tanzania number to be invalid")
	}
}

func TestSMSPartInfo(t *testing.T) {
	if got := GetSMSPartInfo(strings.Repeat("a", 160)); got.Parts != 1 || got.Encoding != EncodingGSM7 {
		t.Fatalf("unexpected GSM-7 info: %+v", got)
	}
	if got := GetSMSPartInfo(strings.Repeat("a", 161)); got.Parts != 2 {
		t.Fatalf("expected 2 GSM-7 parts, got %+v", got)
	}
	if got := GetSMSPartInfo("Hello 😊"); got.Encoding != EncodingUCS2 || got.Parts != 1 {
		t.Fatalf("unexpected UCS-2 info: %+v", got)
	}
	if got := GetSMSPartInfo("^{}"); got.Length != 6 {
		t.Fatalf("expected GSM-7 extension characters to count as 6 septets, got %+v", got)
	}
}

func TestSendSMS(t *testing.T) {
	var gotRequestID, gotIdempotency, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sms/" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotRequestID = r.Header.Get("X-Request-Id")
		gotIdempotency = r.Header.Get("Idempotency-Key")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"success":true,"data":{"message_id":"SA-123","status":"sent","cost":"TZS 35.00","credits_used":1},"request_id":"req-1"}`)
	}))
	defer srv.Close()

	client := NewClient("SA-test", WithBaseURL(srv.URL+"/v1"), WithMaxRetries(0))
	got, err := client.SendSMS(context.Background(), SendSMSRequest{To: "0712345678", Message: "Hello", Sender: "MyBrand"}, RequestOptions{IdempotencyKey: "order-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "SA-123" || got.CreditsUsed != 1 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if !strings.Contains(gotBody, `"to":"+255712345678"`) {
		t.Fatalf("phone was not normalized in body: %s", gotBody)
	}
	if gotIdempotency != "order-1" || gotRequestID == "" {
		t.Fatalf("missing tracing/idempotency headers: request_id=%q idempotency=%q", gotRequestID, gotIdempotency)
	}
}

func TestPublicVoucherRateDoesNotRequireAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"success":true,"data":{"min_amount_tzs":1000,"tiers":[]}}`)
	}))
	defer srv.Close()
	client := NewClient("", WithBaseURL(srv.URL+"/v1"), WithMaxRetries(0))
	got, err := client.GetVoucherRate(context.Background())
	if err != nil || got.MinAmountTZS != 1000 {
		t.Fatalf("unexpected voucher rate: %+v, %v", got, err)
	}
}

func TestRetryOnServerError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"success":false,"error":{"code":"server_error","message":"try again"}}`)
			return
		}
		io.WriteString(w, `{"success":true,"data":{"account_id":"acc","balance":7}}`)
	}))
	defer srv.Close()
	client := NewClient("SA-test", WithBaseURL(srv.URL+"/v1"), WithMaxRetries(2))
	got, err := client.GetBalance(context.Background())
	if err != nil || got.Balance != 7 {
		t.Fatalf("unexpected retry result: %+v, %v", got, err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 requests, got %d", calls.Load())
	}
}

func TestAPIErrorClassification(t *testing.T) {
	e := &APIError{StatusCode: http.StatusPaymentRequired, Code: "insufficient_credits"}
	if !e.IsInsufficientCredits() {
		t.Fatal("expected insufficient credit classification")
	}
	if err := NewClient("").requireAuth(); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func TestWebhookSignatureAndParse(t *testing.T) {
	secret, payload := "secret", []byte(`{"id":"delivery-report","status":"Success","phoneNumber":"+255712345678"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	signature := hex.EncodeToString(mac.Sum(nil))
	event, err := ParseWebhook(payload, "sha256="+signature, secret)
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "sms.delivered" || event.Raw == nil {
		t.Fatalf("unexpected event: %+v", event)
	}
	if err := VerifyWebhookSignature(payload, signature+"0", secret); err == nil {
		t.Fatal("expected signature mismatch")
	}
}

func TestParseRetryAfter(t *testing.T) {
	if parseRetryAfter("2") != 2*time.Second {
		t.Fatal("expected seconds-form Retry-After")
	}
}

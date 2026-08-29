package sendafrica

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func io_write(w io.Writer, s string) { _, _ = io.WriteString(w, s) }

func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		io_write(w, `{"status":"ok"}`)
	}))
	defer srv.Close()
	client := NewClient("", WithBaseURL(srv.URL+"/v1"))
	got, err := client.Health(context.Background())
	if err != nil || got.Status != "ok" {
		t.Fatalf("unexpected health: %+v, %v", got, err)
	}
}

func TestGetRates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rates" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		io_write(w, `{"success":true,"data":[{"name":"Tanzania","iso2":"TZ","dial_code":"+255","rate_tzs":35}],"timestamp":"x"}`)
	}))
	defer srv.Close()
	client := NewClient("", WithBaseURL(srv.URL+"/v1"))
	got, err := client.GetRates(context.Background())
	if err != nil || len(got) != 1 || got[0].RateTZS != 35 {
		t.Fatalf("unexpected rates: %+v, %v", got, err)
	}
}

func TestGetCountryRate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rates/KE" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		io_write(w, `{"success":true,"data":{"name":"Kenya","iso2":"KE","dial_code":"+254","rate_tzs":306}}`)
	}))
	defer srv.Close()
	client := NewClient("", WithBaseURL(srv.URL+"/v1"))
	got, err := client.GetCountryRate(context.Background(), "KE")
	if err != nil || got.RateTZS != 306 {
		t.Fatalf("unexpected rate: %+v, %v", got, err)
	}
}

func TestAuthRegisterAndLogin(t *testing.T) {
	var registerBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/register":
			b, _ := readBody(r)
			registerBody = string(b)
			io_write(w, `{"success":true,"data":{"account_id":"abc","email":"j@x.com","message":"verify email"}}`)
		case "/v1/auth/login":
			io_write(w, `{"success":true,"data":{"access_token":"AT","refresh_token":"RT","token_type":"Bearer","expires_in":900}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	client := NewClient("", WithBaseURL(srv.URL+"/v1"))
	reg, err := client.Register(context.Background(), RegisterRequest{
		FirstName: "John", LastName: "Doe", Email: "j@x.com",
		Password: "a-strong-password-123", Phone: "0712345678",
	})
	if err != nil || reg.AccountID != "abc" {
		t.Fatalf("unexpected register: %+v, %v", reg, err)
	}
	if !strings.Contains(registerBody, `"phone":"+255712345678"`) {
		t.Fatalf("phone not normalized in register body: %s", registerBody)
	}
	pair, err := client.Login(context.Background(), "j@x.com", "secret")
	if err != nil || pair.AccessToken != "AT" || pair.RefreshToken != "RT" {
		t.Fatalf("unexpected login: %+v, %v", pair, err)
	}
}

func TestAuthProfileAndAPIKeys(t *testing.T) {
	var lastAuth, lastMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth = r.Header.Get("Authorization")
		lastMethod = r.Method
		switch r.URL.Path {
		case "/v1/auth/me":
			if lastMethod == http.MethodGet {
				io_write(w, `{"success":true,"data":{"id":"u1","email":"a@b.co","first_name":"A"}}`)
			} else {
				io_write(w, `{"success":true,"data":{"id":"u1","email":"a@b.co","first_name":"B"}}`)
			}
		case "/v1/auth/api-keys":
			io_write(w, `{"success":true,"data":{"id":"k1","name":"prod","key":"SA-abc","created_at":"2026-01-01T00:00:00Z"}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	client := NewClient("", WithBaseURL(srv.URL+"/v1"), WithBearerToken("JWT-token"))
	me, err := client.Me(context.Background())
	if err != nil || me.FirstName != "A" {
		t.Fatalf("unexpected me: %+v, %v", me, err)
	}
	if lastAuth != "Bearer JWT-token" {
		t.Fatalf("expected bearer auth, got %q", lastAuth)
	}
	me2, err := client.UpdateMe(context.Background(), UpdateProfileRequest{FirstName: "B"})
	if err != nil || me2.FirstName != "B" {
		t.Fatalf("unexpected update: %+v, %v", me2, err)
	}
	key, err := client.CreateAPIKey(context.Background(), "prod")
	if err != nil || key.Key != "SA-abc" {
		t.Fatalf("unexpected api key: %+v, %v", key, err)
	}
}

func TestAuthRequiresCredential(t *testing.T) {
	client := NewClient("", WithMaxRetries(0))
	if _, err := client.Me(context.Background()); err == nil || !strings.Contains(err.Error(), ErrMissingAPIKey.Error()) {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func TestContactListsAndContacts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/contact-lists/":
			if r.Method == http.MethodGet {
				io_write(w, `{"success":true,"data":[{"id":1,"name":"list"}]}`)
			} else {
				io_write(w, `{"success":true,"data":{"id":2,"name":"new"}}`)
			}
		case "/v1/contact-lists/2/contacts":
			io_write(w, `{"success":true,"data":{"id":10,"first_name":"Amina","phone":"+255712345678"}}`)
		case "/v1/contact-lists/2/contacts/10":
			io_write(w, `{"success":true,"data":{"id":10,"first_name":"Amina","phone":"+255712345678"}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	client := NewClient("SA-test", WithBaseURL(srv.URL+"/v1"))
	lists, err := client.ListContactLists(context.Background())
	if err != nil || len(lists) != 1 {
		t.Fatalf("unexpected lists: %+v, %v", lists, err)
	}
	created, err := client.CreateContactList(context.Background(), "new")
	if err != nil || created.ID != 2 {
		t.Fatalf("unexpected create: %+v, %v", created, err)
	}
	contact, err := client.AddContact(context.Background(), 2, CreateContactRequest{FirstName: "Amina", Phone: "0712345678"})
	if err != nil || contact.ID != 10 {
		t.Fatalf("unexpected contact: %+v, %v", contact, err)
	}
}

func TestCampaigns(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/campaigns/":
			if r.Method == http.MethodGet {
				io_write(w, `{"success":true,"data":[{"id":42,"name":"June","status":"scheduled"}]}`)
			} else {
				b, _ := readBody(r)
				body = string(b)
				io_write(w, `{"success":true,"data":{"id":42,"name":"June","status":"scheduled","recipients_count":482}}`)
			}
		case "/v1/campaigns/42":
			io_write(w, `{"success":true,"data":{"id":42,"status":"processing","sent":10,"delivered":9,"failed":1}}`)
		case "/v1/campaigns/42/cancel":
			io_write(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	client := NewClient("SA-test", WithBaseURL(srv.URL+"/v1"))
	campaign, err := client.CreateCampaign(context.Background(), CreateCampaignRequest{
		Name: "June", Message: "Hi", ContactListID: 3,
	}, RequestOptions{IdempotencyKey: "promo-june-launch"})
	if err != nil || campaign.ID != 42 || campaign.Status != "scheduled" {
		t.Fatalf("unexpected campaign: %+v, %v", campaign, err)
	}
	if !strings.Contains(body, `"contact_list_id":3`) {
		t.Fatalf("campaign body missing contact_list_id: %s", body)
	}
	getted, err := client.GetCampaign(context.Background(), 42)
	if err != nil || getted.Delivered != 9 {
		t.Fatalf("unexpected get: %+v, %v", getted, err)
	}
	if err := client.CancelCampaign(context.Background(), 42); err != nil {
		t.Fatalf("unexpected cancel: %v", err)
	}
}

func TestNotifications(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/notifications/":
			io_write(w, `{"success":true,"data":{"items":[{"id":91,"type":"payment_confirmed","read":false}],"total":1,"page":1,"per_page":25,"total_pages":1}}`)
		case "/v1/notifications/unread-count":
			io_write(w, `{"success":true,"data":{"count":3}}`)
		case "/v1/notifications/91/read":
			io_write(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	client := NewClient("SA-test", WithBaseURL(srv.URL+"/v1"))
	page, err := client.ListNotifications(context.Background(), PageQuery{Page: 1})
	if err != nil || len(page.Items) != 1 || page.Total != 1 {
		t.Fatalf("unexpected notifications: %+v, %v", page, err)
	}
	count, err := client.UnreadNotificationCount(context.Background())
	if err != nil || count != 3 {
		t.Fatalf("unexpected count: %d, %v", count, err)
	}
	if err := client.MarkNotificationRead(context.Background(), 91); err != nil {
		t.Fatalf("unexpected mark read: %v", err)
	}
}

func TestContactCSVExportImport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/contact-lists/5/contacts/export":
			io_write(w, "first_name,phone\nA,0712345678\n")
		case "/v1/contact-lists/5/import":
			if r.Header.Get("Content-Type") == "" || !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
				t.Errorf("expected multipart content type, got %q", r.Header.Get("Content-Type"))
			}
			io_write(w, `{"success":true,"data":{"imported":2,"skipped":1,"errors":[{"row":3,"error":"invalid_phone_number"}]}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	client := NewClient("SA-test", WithBaseURL(srv.URL+"/v1"))
	csv, err := client.ExportContacts(context.Background(), 5)
	if err != nil || !strings.HasPrefix(string(csv), "first_name") {
		t.Fatalf("unexpected export: %q, %v", csv, err)
	}
	imported, err := client.ImportContactsCSV(context.Background(), 5, []byte("first_name,phone\nA,0712345678\n"))
	if err != nil || imported.Imported != 2 || imported.Skipped != 1 || len(imported.Errors) != 1 {
		t.Fatalf("unexpected import: %+v, %v", imported, err)
	}
}

func TestContactPhonesAndRecipients(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/contact-lists/5/contacts/10/phones":
			io_write(w, `{"success":true,"data":{}}`)
		case "/v1/campaigns/7/recipients":
			io_write(w, `{"success":true,"data":[{"recipient":"+255712345678","status":"sent"}]}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	client := NewClient("SA-test", WithBaseURL(srv.URL+"/v1"))
	if err := client.AddContactPhone(context.Background(), 5, 10, "0754000111"); err != nil {
		t.Fatalf("unexpected add phone: %v", err)
	}
	recips, err := client.ListCampaignRecipients(context.Background(), 7, RecipientQuery{Status: "sent"})
	if err != nil || len(recips) != 1 || recips[0].Status != "sent" {
		t.Fatalf("unexpected recipients: %+v, %v", recips, err)
	}
}

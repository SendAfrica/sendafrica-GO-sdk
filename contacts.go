package sendafrica

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
)

// Contact models and methods. These are dashboard-facing routes and follow the
// bearer-token (JWT) or API-key authentication modes accepted by the client.

type ContactList struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	ContactsCount int    `json:"contacts_count,omitempty"`
}

// ListContactLists returns the account's contact lists.
func (c *Client) ListContactLists(ctx context.Context) ([]ContactList, error) {
	if err := c.requireAuth(); err != nil {
		return nil, err
	}
	var out []ContactList
	_, err := c.do(ctx, http.MethodGet, "/contact-lists/", nil, nil, &out, RequestOptions{})
	return out, err
}

// CreateContactList creates a named contact list.
func (c *Client) CreateContactList(ctx context.Context, name string) (ContactList, error) {
	if err := c.requireAuth(); err != nil {
		return ContactList{}, err
	}
	var out ContactList
	_, err := c.do(ctx, http.MethodPost, "/contact-lists/", nil, map[string]string{"name": name}, &out, RequestOptions{})
	return out, err
}

type Contact struct {
	ID        int    `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	Email     string `json:"email,omitempty"`
	OptedOut  bool   `json:"opted_out,omitempty"`
}

type ContactQuery struct {
	Page    int
	PerPage int
	Search  string
}

func (q ContactQuery) values() url.Values {
	v := (PageQuery{Page: q.Page, PerPage: q.PerPage}).values()
	if q.Search != "" {
		v.Set("search", q.Search)
	}
	return v
}

// ListContacts lists (optionally searchable) contacts in a list.
func (c *Client) ListContacts(ctx context.Context, listID int, query ContactQuery) ([]Contact, error) {
	if err := c.requireAuth(); err != nil {
		return nil, err
	}
	var out []Contact
	_, err := c.do(ctx, http.MethodGet, "/contact-lists/"+strconv.Itoa(listID)+"/contacts", query.values(), nil, &out, RequestOptions{})
	return out, err
}

type CreateContactRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	Email     string `json:"email,omitempty"`
}

// AddContact adds a contact to a list, normalizing the phone. A phone already
// present in the list returns a 409 duplicate_contact error.
func (c *Client) AddContact(ctx context.Context, listID int, request CreateContactRequest) (Contact, error) {
	if err := c.requireAuth(); err != nil {
		return Contact{}, err
	}
	normalized, err := NormalizeTZPhone(request.Phone)
	if err != nil {
		return Contact{}, err
	}
	request.Phone = normalized
	var out Contact
	_, err = c.do(ctx, http.MethodPost, "/contact-lists/"+strconv.Itoa(listID)+"/contacts", nil, request, &out, RequestOptions{})
	return out, err
}

// GetContact fetches a single contact in a list.
func (c *Client) GetContact(ctx context.Context, listID, contactID int) (Contact, error) {
	if err := c.requireAuth(); err != nil {
		return Contact{}, err
	}
	var out Contact
	_, err := c.do(ctx, http.MethodGet, "/contact-lists/"+strconv.Itoa(listID)+"/contacts/"+strconv.Itoa(contactID), nil, nil, &out, RequestOptions{})
	return out, err
}

// UpdateContact updates a contact's fields, including phone, in a list.
func (c *Client) UpdateContact(ctx context.Context, listID, contactID int, request CreateContactRequest) (Contact, error) {
	if err := c.requireAuth(); err != nil {
		return Contact{}, err
	}
	if request.Phone != "" {
		normalized, err := NormalizeTZPhone(request.Phone)
		if err != nil {
			return Contact{}, err
		}
		request.Phone = normalized
	}
	var out Contact
	_, err := c.do(ctx, http.MethodPut, "/contact-lists/"+strconv.Itoa(listID)+"/contacts/"+strconv.Itoa(contactID), nil, request, &out, RequestOptions{})
	return out, err
}

// DeleteContact removes a contact from a list.
func (c *Client) DeleteContact(ctx context.Context, listID, contactID int) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodDelete, "/contact-lists/"+strconv.Itoa(listID)+"/contacts/"+strconv.Itoa(contactID), nil, nil, nil, RequestOptions{})
	return err
}

// AddContactPhone attaches an additional phone number to a contact.
func (c *Client) AddContactPhone(ctx context.Context, listID, contactID int, phone string) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	normalized, err := NormalizeTZPhone(phone)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, "/contact-lists/"+strconv.Itoa(listID)+"/contacts/"+strconv.Itoa(contactID)+"/phones", nil, map[string]string{"phone": normalized}, nil, RequestOptions{})
	return err
}

// DeleteContactPhone removes an additional phone number from a contact.
func (c *Client) DeleteContactPhone(ctx context.Context, listID, contactID, phoneID int) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodDelete, "/contact-lists/"+strconv.Itoa(listID)+"/contacts/"+strconv.Itoa(contactID)+"/phones/"+strconv.Itoa(phoneID), nil, nil, nil, RequestOptions{})
	return err
}

// ExportContacts returns the contacts of a list as raw CSV bytes.
func (c *Client) ExportContacts(ctx context.Context, listID int) ([]byte, error) {
	if err := c.requireAuth(); err != nil {
		return nil, err
	}
	endpoint := joinURL(c.baseURL, "/contact-lists/"+strconv.Itoa(listID)+"/contacts/export")
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil, RequestOptions{})
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: body, Headers: resp.Header.Clone()}
	}
	return body, nil
}

type ImportError struct {
	Row   int    `json:"row"`
	Error string `json:"error"`
}

type ImportResult struct {
	Imported int           `json:"imported"`
	Skipped  int           `json:"skipped"`
	Errors   []ImportError `json:"errors"`
}

// ImportContactsCSV performs a bulk CSV import into a list via multipart
// upload. The file field is named "file". It returns a row-level error report.
func (c *Client) ImportContactsCSV(ctx context.Context, listID int, csvData []byte) (ImportResult, error) {
	if err := c.requireAuth(); err != nil {
		return ImportResult{}, err
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "contacts.csv")
	if err != nil {
		return ImportResult{}, err
	}
	if _, err := part.Write(csvData); err != nil {
		return ImportResult{}, err
	}
	if err := writer.Close(); err != nil {
		return ImportResult{}, err
	}

	endpoint := joinURL(c.baseURL, "/contact-lists/"+strconv.Itoa(listID)+"/import")
	req, err := c.newRequest(ctx, http.MethodPost, endpoint, nil, RequestOptions{})
	if err != nil {
		return ImportResult{}, err
	}
	req.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
	req.ContentLength = int64(buf.Len())
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ImportResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ImportResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var out ImportResult
		_ = json.Unmarshal(body, &out)
		return out, &APIError{StatusCode: resp.StatusCode, Body: body, Headers: resp.Header.Clone()}
	}
	var envelope struct {
		Data ImportResult `json:"data"`
	}
	_ = json.Unmarshal(body, &envelope)
	return envelope.Data, nil
}

type GoogleContactsStatus struct {
	Connected bool `json:"connected"`
}

// GetGoogleContactsStatus reports whether a Google Contacts sync connection exists.
func (c *Client) GetGoogleContactsStatus(ctx context.Context) (GoogleContactsStatus, error) {
	if err := c.requireAuth(); err != nil {
		return GoogleContactsStatus{}, err
	}
	var out GoogleContactsStatus
	_, err := c.do(ctx, http.MethodGet, "/contact-lists/google/status", nil, nil, &out, RequestOptions{})
	return out, err
}

// GoogleContactsSync triggers a one-way Google Contacts import.
func (c *Client) GoogleContactsSync(ctx context.Context) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPost, "/contact-lists/google/sync", nil, nil, nil, RequestOptions{})
	return err
}

// GoogleContactsDisconnect disconnects an existing Google Contacts connection.
func (c *Client) GoogleContactsDisconnect(ctx context.Context) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPost, "/contact-lists/google/disconnect", nil, nil, nil, RequestOptions{})
	return err
}

package sendafrica

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Campaign models and methods for scheduled bulk SMS against a contact list.

type CreateCampaignRequest struct {
	Name          string    `json:"name"`
	Message       string    `json:"message"`
	ContactListID int       `json:"contact_list_id"`
	ScheduledAt   time.Time `json:"scheduled_at"`
	SenderID      string    `json:"sender_id,omitempty"`
}

type Campaign struct {
	ID               int       `json:"id"`
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	RecipientsCount  int       `json:"recipients_count"`
	EstimatedCredits int       `json:"estimated_credits"`
	ScheduledAt      time.Time `json:"scheduled_at"`
	TotalRecipients  int       `json:"total_recipients"`
	Sent             int       `json:"sent"`
	Delivered        int       `json:"delivered"`
	Failed           int       `json:"failed"`
	Pending          int       `json:"pending"`
	CreditsSpent     int       `json:"credits_spent"`
	CostTZS          int       `json:"cost_tzs"`
	CreatedAt        time.Time `json:"created_at"`
}

// ListCampaigns lists campaigns with live stats.
func (c *Client) ListCampaigns(ctx context.Context) ([]Campaign, error) {
	if err := c.requireAuth(); err != nil {
		return nil, err
	}
	var out []Campaign
	_, err := c.do(ctx, http.MethodGet, "/campaigns/", nil, nil, &out, RequestOptions{})
	return out, err
}

// CreateCampaign creates and schedules a campaign. Pass a deterministic
// idempotency key via opts to make creation replay-safe.
func (c *Client) CreateCampaign(ctx context.Context, request CreateCampaignRequest, opts ...RequestOptions) (Campaign, error) {
	if err := c.requireAuth(); err != nil {
		return Campaign{}, err
	}
	var out Campaign
	_, err := c.do(ctx, http.MethodPost, "/campaigns/", nil, request, &out, firstRequestOptions(opts))
	return out, err
}

// GetCampaign returns a single campaign with live stats.
func (c *Client) GetCampaign(ctx context.Context, id int) (Campaign, error) {
	if err := c.requireAuth(); err != nil {
		return Campaign{}, err
	}
	var out Campaign
	_, err := c.do(ctx, http.MethodGet, "/campaigns/"+strconv.Itoa(id), nil, nil, &out, RequestOptions{})
	return out, err
}

// CancelCampaign cancels a draft or scheduled campaign.
func (c *Client) CancelCampaign(ctx context.Context, id int) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPost, "/campaigns/"+strconv.Itoa(id)+"/cancel", nil, nil, nil, RequestOptions{})
	return err
}

// DeleteCampaign deletes a campaign that never sent anything.
func (c *Client) DeleteCampaign(ctx context.Context, id int) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodDelete, "/campaigns/"+strconv.Itoa(id), nil, nil, nil, RequestOptions{})
	return err
}

type RecipientQuery struct {
	Status  string
	Page    int
	PerPage int
}

func (q RecipientQuery) values() url.Values {
	v := (PageQuery{Page: q.Page, PerPage: q.PerPage}).values()
	if q.Status != "" {
		v.Set("status", q.Status)
	}
	return v
}

type CampaignRecipient struct {
	Recipient string `json:"recipient"`
	Status    string `json:"status"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ListCampaignRecipients returns per-recipient tracking, optionally filtered
// by status (sent, delivered, failed, or pending).
func (c *Client) ListCampaignRecipients(ctx context.Context, id int, query RecipientQuery) ([]CampaignRecipient, error) {
	if err := c.requireAuth(); err != nil {
		return nil, err
	}
	var out []CampaignRecipient
	_, err := c.do(ctx, http.MethodGet, "/campaigns/"+strconv.Itoa(id)+"/recipients", query.values(), nil, &out, RequestOptions{})
	return out, err
}

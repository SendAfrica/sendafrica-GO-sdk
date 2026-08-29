package sendafrica

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// Notification models and methods for in-app alerts: low-balance warnings,
// payment confirmations, campaign completions, and platform announcements.

type Notification struct {
	ID        int       `json:"id"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

type Notifications struct {
	Items      []Notification `json:"items"`
	Total      int            `json:"total"`
	Page       int            `json:"page"`
	PerPage    int            `json:"per_page"`
	TotalPages int            `json:"total_pages"`
}

// ListNotifications lists paginated notifications.
func (c *Client) ListNotifications(ctx context.Context, q PageQuery) (Notifications, error) {
	if err := c.requireAuth(); err != nil {
		return Notifications{}, err
	}
	var out Notifications
	_, err := c.do(ctx, http.MethodGet, "/notifications/", q.values(), nil, &out, RequestOptions{})
	return out, err
}

type UnreadCount struct {
	Count int `json:"count"`
}

// UnreadNotificationCount returns the unread badge count.
func (c *Client) UnreadNotificationCount(ctx context.Context) (int, error) {
	if err := c.requireAuth(); err != nil {
		return 0, err
	}
	var out UnreadCount
	_, err := c.do(ctx, http.MethodGet, "/notifications/unread-count", nil, nil, &out, RequestOptions{})
	return out.Count, err
}

// MarkNotificationRead marks a single notification as read.
func (c *Client) MarkNotificationRead(ctx context.Context, id int) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPatch, "/notifications/"+strconv.Itoa(id)+"/read", nil, nil, nil, RequestOptions{})
	return err
}

// MarkAllNotificationsRead marks every notification as read.
func (c *Client) MarkAllNotificationsRead(ctx context.Context) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPost, "/notifications/read-all", nil, nil, nil, RequestOptions{})
	return err
}

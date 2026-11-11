// Package userclient talks to the user service's internal API.
package userclient

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/fayrly/backend/internal/platform/internalhttp"
)

type Client struct {
	http *internalhttp.Client
}

func New(baseURL, internalKey string, timeout time.Duration) *Client {
	return &Client{http: internalhttp.New(baseURL, internalKey, timeout)}
}

type Profile struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

type EnsureInput struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Phone string `json:"phone,omitempty"`
	Role  string `json:"role,omitempty"`
}

func (c *Client) EnsureUser(ctx context.Context, input EnsureInput) (*Profile, error) {
	var profile Profile
	if err := c.http.Do(ctx, http.MethodPost, "/internal/users/ensure", input, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

func (c *Client) GetUser(ctx context.Context, id string) (*Profile, error) {
	var profile Profile
	if err := c.http.Do(ctx, http.MethodGet, "/internal/users/"+url.PathEscape(id), nil, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

func (c *Client) GetUserByEmail(ctx context.Context, email string) (*Profile, error) {
	var profile Profile
	path := "/internal/users/by-email?email=" + url.QueryEscape(email)
	if err := c.http.Do(ctx, http.MethodGet, path, nil, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

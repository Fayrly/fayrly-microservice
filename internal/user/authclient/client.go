// Package authclient talks to the auth service's internal credential API.
package authclient

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/internalhttp"
)

type Client struct {
	http *internalhttp.Client
}

func New(baseURL, internalKey string, timeout time.Duration) *Client {
	return &Client{http: internalhttp.New(baseURL, internalKey, timeout)}
}

type CreateCredentialInput struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	Password string `json:"password,omitempty"`
}

type credentialResponse struct {
	TemporaryPassword string `json:"temporary_password"`
}

// CreateCredential provisions login credentials. When password is empty the
// auth service generates one and returns it.
func (c *Client) CreateCredential(ctx context.Context, input CreateCredentialInput) (string, error) {
	var response credentialResponse
	if err := c.http.Do(ctx, http.MethodPost, "/internal/credentials", input, &response); err != nil {
		return "", err
	}
	return response.TemporaryPassword, nil
}

func (c *Client) DeleteCredential(ctx context.Context, userID string) error {
	err := c.http.Do(ctx, http.MethodDelete, "/internal/credentials/"+userID, nil, nil)
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return nil
	}
	return err
}

func (c *Client) ResetPassword(ctx context.Context, userID string) (string, error) {
	var response credentialResponse
	if err := c.http.Do(ctx, http.MethodPost, "/internal/credentials/"+userID+"/reset-password", nil, &response); err != nil {
		return "", err
	}
	return response.TemporaryPassword, nil
}

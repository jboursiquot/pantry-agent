package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type doer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	webhookURL string
	httpClient doer
}

func NewClient(webhookURL string, httpClient doer) *Client {
	return &Client{
		webhookURL: webhookURL,
		httpClient: httpClient,
	}
}

func (c *Client) PostMessage(ctx context.Context, channel string, message string) error {
	payload, err := json.Marshal(map[string]any{
		"channel": channel,
		"text":    message,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to post message: %s", resp.Status)
	}

	return nil
}

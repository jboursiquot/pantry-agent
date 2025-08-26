package slack_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"pantryagent/slack"

	should "github.com/stretchr/testify/assert"
	must "github.com/stretchr/testify/require"
)

type mockDoer struct {
	resp   *http.Response
	err    error
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockDoer) Do(req *http.Request) (*http.Response, error) {
	if m.doFunc != nil {
		return m.doFunc(req)
	}
	return m.resp, m.err
}

func TestNewClient(t *testing.T) {
	webhook := "http://slack.com/webhook"
	client := slack.NewClient(webhook, &mockDoer{})
	must.NotNil(t, client, "expected non-nil client")
}

func TestPostMessage(t *testing.T) {
	tests := []struct {
		name    string
		doFunc  func(req *http.Request) (*http.Response, error)
		wantErr error
	}{
		{
			name: "success",
			doFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString("ok"))}, nil
			},
			wantErr: nil,
		},
		{
			name: "failure status",
			doFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusBadRequest, Status: "400 Bad Request", Body: io.NopCloser(bytes.NewBufferString("bad request"))}, nil
			},
			wantErr: fmt.Errorf("failed to post message: 400 Bad Request"),
		},
		{
			name: "do error",
			doFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network error")
			},
			wantErr: fmt.Errorf("network error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := slack.NewClient("http://example.com/webhook", &mockDoer{doFunc: tt.doFunc})
			err := client.PostMessage(context.Background(), "#general", "Hello, world!")
			should.Equal(t, tt.wantErr, err)
		})
	}
}

package cursor

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	aiserverv1 "github.com/bengu3/cursor-tab.nvim/cursor-api/gen/aiserver/v1"
	"github.com/bengu3/cursor-tab.nvim/cursor-api/gen/aiserver/v1/aiserverv1connect"
)

const APIBaseURL = "https://api4.cursor.sh"

type Client struct {
	aiClient      aiserverv1connect.AiServiceClient
	accessToken   string
	machineID     string
	clientVersion string
}

func NewClient() (*Client, error) {
	accessToken, err := GetAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	machineID, err := GetMachineID()
	if err != nil {
		return nil, fmt.Errorf("failed to get machine ID: %w", err)
	}

	clientVersion, err := GetCursorVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get Cursor version: %w", err)
	}

	httpClient := &http.Client{}
	aiClient := aiserverv1connect.NewAiServiceClient(httpClient, APIBaseURL)

	return &Client{
		aiClient:      aiClient,
		accessToken:   accessToken,
		machineID:     machineID,
		clientVersion: clientVersion,
	}, nil
}

func (c *Client) StreamCpp(ctx context.Context, req *aiserverv1.StreamCppRequest) (*connect.ServerStreamForClient[aiserverv1.StreamCppResponse], error) {
	connectReq := connect.NewRequest(req)
	connectReq.Header().Set("authorization", "Bearer "+c.accessToken)
	connectReq.Header().Set("x-cursor-client-version", c.clientVersion)

	stream, err := c.aiClient.StreamCpp(ctx, connectReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call StreamCpp: %w", err)
	}

	return stream, nil
}

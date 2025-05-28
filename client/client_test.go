package client

import (
	"context"
	"github.com/mark3labs/mcp-go/mcp"
	"testing"
	"time"
)

func TestStreamableHTTPClient(t *testing.T) {
	client, err := NewStreamableHttpClient("http://localhost:8080/mcp")
	if err != nil {
		t.Fatalf("create client failed %v", err)
		return
	}

	client.OnNotification(func(notification mcp.JSONRPCNotification) {
		t.Logf("receive notification %v", notification)
	})

	ctx := context.Background()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Failed to start client: %v", err)
		return
	}

	// Initialize
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}

	_, err = client.Initialize(ctx, initRequest)
	if err != nil {
		t.Fatalf("Failed to initialize: %v\n", err)
	}

	request := mcp.CallToolRequest{}
	request.Params.Name = "notify"
	result, err := client.CallTool(ctx, request)
	println("111111")
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	// do NOT shutdown immediately, wait to receive notification successfully
	time.Sleep(time.Second * 10)
	if len(result.Content) != 1 {
		t.Errorf("Expected 1 content item, got %d", len(result.Content))
	}
}

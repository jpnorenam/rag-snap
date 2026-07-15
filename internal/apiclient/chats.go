package apiclient

import (
	"context"
	"net/url"

	"github.com/jpnorenam/rag-snap/internal/chatstore"
)

// ListChats returns saved chat summaries newest-first. A non-empty search filters
// server-side by case-insensitive substring over title and transcript content.
func (c *Client) ListChats(ctx context.Context, search string) ([]chatstore.Summary, error) {
	path := "/1.0/chats"
	if search != "" {
		path += "?search=" + url.QueryEscape(search)
	}
	var out []chatstore.Summary
	if err := c.Sync(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetChat returns a full saved chat, including its transcript.
func (c *Client) GetChat(ctx context.Context, id string) (*chatstore.Chat, error) {
	var chat chatstore.Chat
	if err := c.Sync(ctx, "GET", "/1.0/chats/"+id, nil, &chat); err != nil {
		return nil, err
	}
	return &chat, nil
}

// DeleteChat removes a saved chat.
func (c *Client) DeleteChat(ctx context.Context, id string) error {
	return c.Sync(ctx, "DELETE", "/1.0/chats/"+id, nil, nil)
}

package telegram

import (
	"context"
	"time"
)

const MessageLinkSourceType = "telegram_message_links"
const ChannelDocumentSourceType = "telegram_channel_document"

type ResolveInput struct {
	URL      string
	Username string
}

type ChannelRef struct {
	ID         *int64
	AccessHash *int64
	Username   string
	Title      string
	URL        string
}

type FetchOptions struct {
	Limit          int
	AfterMessageID *int64
	Cursor         *string
}

type FetchPage struct {
	Messages   []Message
	NextCursor *string
}

type MessageLink struct {
	OriginalURL  string
	CanonicalURL string
	Username     string
	ChannelID    *int64
	MessageID    int64
}

type Message struct {
	MessageID        int64
	GroupedID        *int64
	PostedAt         *time.Time
	EditedAt         *time.Time
	TextRaw          string
	CaptionRaw       string
	RawJSON          map[string]any
	MediaJSON        map[string]any
	EntitiesJSON     map[string]any
	ReactionsJSON    map[string]any
	ViewsCount       *int
	ForwardsCount    *int
	ReplyToMessageID *int64
	ForwardFromName  *string
	ForwardFromChat  *string
	HasMedia         bool
}

type Collector interface {
	ResolveChannel(ctx context.Context, input ResolveInput) (ChannelRef, error)
	ResolveChannelByID(ctx context.Context, channelID int64) (ChannelRef, error)
	FetchChannelHistory(ctx context.Context, channel ChannelRef, opts FetchOptions) (FetchPage, error)
	FetchMessages(ctx context.Context, channel ChannelRef, messageIDs []int64) ([]Message, error)
}

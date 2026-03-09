package telegram

import (
	"context"
	"strings"
	"time"
)

type StubCollector struct{}

func NewStubCollector() *StubCollector {
	return &StubCollector{}
}

func (c *StubCollector) ResolveChannel(ctx context.Context, input ResolveInput) (ChannelRef, error) {
	_ = ctx
	username, sourceURL, err := ResolveUsername(input)
	if err != nil {
		return ChannelRef{}, err
	}

	channelID := int64(900000 + len(username))
	accessHash := int64(700000 + len(username)*17)

	return ChannelRef{
		ID:         &channelID,
		AccessHash: &accessHash,
		Username:   username,
		Title:      "@" + username,
		URL:        sourceURL,
	}, nil
}

func (c *StubCollector) FetchChannelHistory(ctx context.Context, channel ChannelRef, opts FetchOptions) (FetchPage, error) {
	_ = ctx
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	const maxHistoryMessages = int64(2000)

	minID := int64(0)
	if opts.AfterMessageID != nil {
		minID = *opts.AfterMessageID
	}
	cursorID := maxHistoryMessages
	if opts.Cursor != nil && strings.TrimSpace(*opts.Cursor) != "" {
		parsed, ok := parseInt64(strings.TrimSpace(*opts.Cursor))
		if !ok {
			return FetchPage{}, nil
		}
		cursorID = parsed
	}

	if cursorID <= minID {
		return FetchPage{}, nil
	}

	now := time.Now().UTC()
	startID := cursorID
	endID := cursorID - int64(limit) + 1
	if endID < (minID + 1) {
		endID = minID + 1
	}

	capSize := int(startID-endID) + 1
	if capSize < 0 {
		capSize = 0
	}
	messages := make([]Message, 0, capSize)
	for msgID := startID; msgID >= endID; msgID-- {
		if msgID%17 == 0 {
			// simulate deleted/service-like message
			messages = append(messages, Message{
				MessageID: msgID,
				TextRaw:   "   ",
				RawJSON: map[string]any{
					"kind": "service",
				},
			})
			continue
		}

		base := "Update from @" + channel.Username + ": release " + itoa64(msgID) + ". " +
			"Read: https://example.com/post/" + itoa64(msgID) + "?utm_source=tg #release @team"
		if msgID%9 == 0 {
			base = "🔥🔥🔥 " + base + " 🚀🚀🚀"
		}
		if msgID%11 == 0 {
			base = "Duplicate message template with link https://example.com/dup #dup"
		}

		views := int(msgID * 10)
		forwards := int(msgID % 5)
		posted := now.Add(-time.Duration(maxHistoryMessages-msgID) * time.Hour)
		isForward := msgID%8 == 0

		msg := Message{
			MessageID:     msgID,
			PostedAt:      &posted,
			TextRaw:       strings.TrimSpace(base),
			CaptionRaw:    "",
			RawJSON:       map[string]any{"message_id": msgID, "source": "stub"},
			EntitiesJSON:  map[string]any{"hashtags": []string{"release"}},
			ViewsCount:    &views,
			ForwardsCount: &forwards,
			HasMedia:      msgID%4 == 0,
		}

		if msg.HasMedia {
			msg.MediaJSON = map[string]any{"type": "photo"}
			msg.CaptionRaw = "Image caption for " + itoa64(msgID) + " https://img.example.com/" + itoa64(msgID)
		}

		if isForward {
			forwardName := "Forwarded Author"
			forwardChat := "@origin_channel"
			msg.ForwardFromName = &forwardName
			msg.ForwardFromChat = &forwardChat
		}

		messages = append(messages, msg)
	}

	var nextCursor *string
	if endID > (minID + 1) {
		value := itoa64(endID - 1)
		nextCursor = &value
	}

	return FetchPage{
		Messages:   messages,
		NextCursor: nextCursor,
	}, nil
}

func parseInt64(v string) (int64, bool) {
	if v == "" {
		return 0, false
	}
	sign := int64(1)
	if strings.HasPrefix(v, "-") {
		sign = -1
		v = strings.TrimPrefix(v, "-")
	}
	if v == "" {
		return 0, false
	}
	var n int64
	for _, r := range v {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = (n * 10) + int64(r-'0')
	}
	return n * sign, true
}

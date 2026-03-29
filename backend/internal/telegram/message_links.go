package telegram

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func ParseMessageLink(raw string) (MessageLink, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return MessageLink{}, errors.New("message link is empty")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return MessageLink{}, fmt.Errorf("invalid telegram message link %q", raw)
	}

	host := strings.ToLower(parsed.Hostname())
	if host != "t.me" && host != "www.t.me" {
		return MessageLink{}, fmt.Errorf("unsupported telegram host in %q", raw)
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return MessageLink{}, fmt.Errorf("telegram message link must include channel and message id: %q", raw)
	}

	if parts[0] == "c" {
		if len(parts) < 3 {
			return MessageLink{}, fmt.Errorf("private telegram message link must include channel id and message id: %q", raw)
		}

		channelID, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil || channelID <= 0 {
			return MessageLink{}, fmt.Errorf("invalid private channel id in %q", raw)
		}

		messageID, err := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		if err != nil || messageID <= 0 {
			return MessageLink{}, fmt.Errorf("invalid telegram message id in %q", raw)
		}

		canonical := PrivateMessageURL(channelID, messageID)
		return MessageLink{
			OriginalURL:  raw,
			CanonicalURL: canonical,
			ChannelID:    &channelID,
			MessageID:    messageID,
		}, nil
	}

	username := normalizeUsername(parts[0])
	if username == "" {
		return MessageLink{}, fmt.Errorf("telegram channel username is missing in %q", raw)
	}

	messageID, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || messageID <= 0 {
		return MessageLink{}, fmt.Errorf("invalid telegram message id in %q", raw)
	}

	canonical := MessageURL(username, messageID)
	return MessageLink{
		OriginalURL:  raw,
		CanonicalURL: canonical,
		Username:     username,
		MessageID:    messageID,
	}, nil
}

func PrivateMessageURL(channelID, messageID int64) string {
	return "https://t.me/c/" + itoa64(channelID) + "/" + itoa64(messageID)
}

func (m MessageLink) IdentityKey() string {
	if m.ChannelID != nil && *m.ChannelID > 0 {
		return "private:" + itoa64(*m.ChannelID)
	}
	return "public:" + normalizeUsername(m.Username)
}

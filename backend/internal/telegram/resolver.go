package telegram

import (
	"errors"
	"net/url"
	"strings"
)

type ChannelLink struct {
	Username      string
	InviteHash    string
	NormalizedURL string
}

func ResolveUsername(input ResolveInput) (string, string, error) {
	link, err := ParseChannelLink(input)
	if err != nil {
		return "", "", err
	}
	if link.Username == "" {
		return "", "", errors.New("only t.me public channel urls are supported")
	}
	return link.Username, link.NormalizedURL, nil
}

func ParseChannelLink(input ResolveInput) (ChannelLink, error) {
	if strings.TrimSpace(input.Username) != "" {
		username := normalizeUsername(input.Username)
		if username == "" {
			return ChannelLink{}, errors.New("channel username is empty")
		}
		return ChannelLink{
			Username:      username,
			NormalizedURL: "https://t.me/" + username,
		}, nil
	}

	rawURL := strings.TrimSpace(input.URL)
	if rawURL == "" {
		return ChannelLink{}, errors.New("either url or username is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ChannelLink{}, errors.New("invalid url")
	}

	host := strings.ToLower(parsed.Hostname())
	if host != "t.me" && host != "www.t.me" {
		return ChannelLink{}, errors.New("only t.me telegram urls are supported")
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return ChannelLink{}, errors.New("telegram channel path is missing in url")
	}

	first := strings.TrimSpace(parts[0])
	if first == "c" {
		return ChannelLink{}, errors.New("telegram private message links are not valid channel links")
	}
	if strings.HasPrefix(first, "+") {
		hash := strings.TrimSpace(strings.TrimPrefix(first, "+"))
		if hash == "" {
			return ChannelLink{}, errors.New("telegram invite hash is missing in url")
		}
		return ChannelLink{
			InviteHash:    hash,
			NormalizedURL: "https://t.me/+" + hash,
		}, nil
	}
	if first == "joinchat" {
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return ChannelLink{}, errors.New("telegram invite hash is missing in url")
		}
		hash := strings.TrimSpace(parts[1])
		return ChannelLink{
			InviteHash:    hash,
			NormalizedURL: "https://t.me/+" + hash,
		}, nil
	}
	if first == "s" {
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return ChannelLink{}, errors.New("telegram channel username is missing in url")
		}
		first = strings.TrimSpace(parts[1])
	}

	username := normalizeUsername(first)
	if username == "" {
		return ChannelLink{}, errors.New("channel username is missing in url")
	}
	return ChannelLink{
		Username:      username,
		NormalizedURL: "https://t.me/" + username,
	}, nil
}

func normalizeUsername(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "@")
	return s
}

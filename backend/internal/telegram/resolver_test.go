package telegram

import "testing"

func TestParseChannelLinkPublic(t *testing.T) {
	link, err := ParseChannelLink(ResolveInput{URL: "https://t.me/SomeChannel/123"})
	if err != nil {
		t.Fatalf("ParseChannelLink returned error: %v", err)
	}

	if link.Username != "somechannel" {
		t.Fatalf("expected username somechannel, got %q", link.Username)
	}
	if link.InviteHash != "" {
		t.Fatalf("expected empty invite hash, got %q", link.InviteHash)
	}
	if link.NormalizedURL != "https://t.me/somechannel" {
		t.Fatalf("unexpected normalized url: %q", link.NormalizedURL)
	}
}

func TestParseChannelLinkInvite(t *testing.T) {
	link, err := ParseChannelLink(ResolveInput{URL: "https://t.me/+AbCdEf"})
	if err != nil {
		t.Fatalf("ParseChannelLink returned error: %v", err)
	}

	if link.InviteHash != "AbCdEf" {
		t.Fatalf("expected invite hash AbCdEf, got %q", link.InviteHash)
	}
	if link.Username != "" {
		t.Fatalf("expected empty username, got %q", link.Username)
	}
	if link.NormalizedURL != "https://t.me/+AbCdEf" {
		t.Fatalf("unexpected normalized url: %q", link.NormalizedURL)
	}
}

func TestParseChannelLinkJoinChat(t *testing.T) {
	link, err := ParseChannelLink(ResolveInput{URL: "https://t.me/joinchat/InviteHash"})
	if err != nil {
		t.Fatalf("ParseChannelLink returned error: %v", err)
	}

	if link.InviteHash != "InviteHash" {
		t.Fatalf("expected invite hash InviteHash, got %q", link.InviteHash)
	}
	if link.NormalizedURL != "https://t.me/+InviteHash" {
		t.Fatalf("unexpected normalized url: %q", link.NormalizedURL)
	}
}

func TestParseChannelLinkSupportsPublicViewURL(t *testing.T) {
	link, err := ParseChannelLink(ResolveInput{URL: "https://t.me/s/SomeChannel"})
	if err != nil {
		t.Fatalf("ParseChannelLink returned error: %v", err)
	}

	if link.Username != "somechannel" {
		t.Fatalf("expected username somechannel, got %q", link.Username)
	}
	if link.NormalizedURL != "https://t.me/somechannel" {
		t.Fatalf("unexpected normalized url: %q", link.NormalizedURL)
	}
}

func TestParseChannelLinkRejectsPrivateMessageLink(t *testing.T) {
	if _, err := ParseChannelLink(ResolveInput{URL: "https://t.me/c/1941234567/42"}); err == nil {
		t.Fatal("expected error for private message link")
	}
}

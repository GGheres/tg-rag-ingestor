package telegram

import "testing"

func TestParseMessageLinkPublic(t *testing.T) {
	link, err := ParseMessageLink("https://t.me/SomeChannel/123?single")
	if err != nil {
		t.Fatalf("ParseMessageLink returned error: %v", err)
	}

	if link.Username != "somechannel" {
		t.Fatalf("expected username somechannel, got %q", link.Username)
	}
	if link.MessageID != 123 {
		t.Fatalf("expected message id 123, got %d", link.MessageID)
	}
	if link.CanonicalURL != "https://t.me/somechannel/123" {
		t.Fatalf("unexpected canonical url: %q", link.CanonicalURL)
	}
	if link.ChannelID != nil {
		t.Fatalf("expected nil channel id for public link")
	}
}

func TestParseMessageLinkPrivate(t *testing.T) {
	link, err := ParseMessageLink("https://t.me/c/1941234567/42")
	if err != nil {
		t.Fatalf("ParseMessageLink returned error: %v", err)
	}

	if link.ChannelID == nil || *link.ChannelID != 1941234567 {
		t.Fatalf("unexpected channel id: %#v", link.ChannelID)
	}
	if link.MessageID != 42 {
		t.Fatalf("expected message id 42, got %d", link.MessageID)
	}
	if link.CanonicalURL != "https://t.me/c/1941234567/42" {
		t.Fatalf("unexpected canonical url: %q", link.CanonicalURL)
	}
}

func TestParseMessageLinkRejectsInvalidURL(t *testing.T) {
	if _, err := ParseMessageLink("https://example.com/channel/1"); err == nil {
		t.Fatal("expected error for non-telegram host")
	}
}

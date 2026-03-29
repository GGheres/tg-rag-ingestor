package telegram

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/session"
	td "github.com/gotd/td/telegram"
	tdauth "github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/query"
	querydialogs "github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"golang.org/x/term"
)

type MTProtoCollector struct {
	APIID       string
	APIHash     string
	Phone       string
	SessionFile string
	Password    string
	AuthCode    string
}

func NewMTProtoCollector(apiID, apiHash, phone, sessionFile, password, authCode string) *MTProtoCollector {
	return &MTProtoCollector{
		APIID:       apiID,
		APIHash:     apiHash,
		Phone:       phone,
		SessionFile: sessionFile,
		Password:    password,
		AuthCode:    authCode,
	}
}

func (c *MTProtoCollector) ResolveChannel(ctx context.Context, input ResolveInput) (ChannelRef, error) {
	channelLink, err := ParseChannelLink(input)
	if err != nil {
		return ChannelRef{}, err
	}

	var out ChannelRef
	err = c.withAPI(ctx, func(ctx context.Context, api *tg.Client) error {
		var resolved ChannelRef
		var err error
		switch {
		case channelLink.Username != "":
			resolved, err = c.resolveChannelByUsername(ctx, api, channelLink.Username, channelLink.NormalizedURL)
		case channelLink.InviteHash != "":
			resolved, err = c.resolveChannelByInviteHash(ctx, api, channelLink.InviteHash, channelLink.NormalizedURL)
		default:
			err = errors.New("unsupported telegram channel input")
		}
		if err != nil {
			return err
		}
		out = resolved
		return nil
	})
	if err != nil {
		return ChannelRef{}, err
	}
	return out, nil
}

func (c *MTProtoCollector) ResolveChannelByID(ctx context.Context, channelID int64) (ChannelRef, error) {
	if channelID <= 0 {
		return ChannelRef{}, errors.New("channel id must be > 0")
	}

	var out ChannelRef
	err := c.withAPI(ctx, func(ctx context.Context, api *tg.Client) error {
		resolved, err := c.resolveChannelByID(ctx, api, channelID)
		if err != nil {
			return err
		}
		out = resolved
		return nil
	})
	if err != nil {
		return ChannelRef{}, err
	}
	return out, nil
}

func (c *MTProtoCollector) FetchChannelHistory(ctx context.Context, channel ChannelRef, opts FetchOptions) (FetchPage, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}

	var page FetchPage
	err := c.withAPI(ctx, func(ctx context.Context, api *tg.Client) error {
		inputPeer, _, err := c.resolveInputPeer(ctx, api, channel)
		if err != nil {
			return err
		}

		req := &tg.MessagesGetHistoryRequest{
			Peer:  inputPeer,
			Limit: limit,
		}

		if opts.AfterMessageID != nil && *opts.AfterMessageID > 0 {
			req.MinID = clampTLInt(*opts.AfterMessageID)
		}
		if opts.Cursor != nil && strings.TrimSpace(*opts.Cursor) != "" {
			offsetID, parseErr := strconv.ParseInt(strings.TrimSpace(*opts.Cursor), 10, 64)
			if parseErr != nil {
				return fmt.Errorf("invalid cursor %q: %w", *opts.Cursor, parseErr)
			}
			if offsetID > 0 {
				req.OffsetID = clampTLInt(offsetID)
			}
		}

		history, err := api.MessagesGetHistory(ctx, req)
		if err != nil {
			return fmt.Errorf("messages.getHistory: %w", err)
		}

		modified, ok := history.AsModified()
		if !ok {
			page = FetchPage{}
			return nil
		}

		classes := modified.GetMessages()
		messages := make([]Message, 0, len(classes))
		var minID int64
		hasMinID := false

		for _, class := range classes {
			switch msg := class.(type) {
			case *tg.Message:
				messages = append(messages, mapTGMessage(msg))
				msgID := int64(msg.GetID())
				if !hasMinID || msgID < minID {
					minID = msgID
					hasMinID = true
				}
			case *tg.MessageService:
				messages = append(messages, mapTGServiceMessage(msg))
				msgID := int64(msg.GetID())
				if !hasMinID || msgID < minID {
					minID = msgID
					hasMinID = true
				}
			}
		}

		var nextCursor *string
		if hasMinID && len(classes) >= limit {
			minAllowed := int64(0)
			if opts.AfterMessageID != nil && *opts.AfterMessageID > 0 {
				minAllowed = *opts.AfterMessageID
			}
			if minID > minAllowed {
				next := itoa64(minID)
				nextCursor = &next
			}
		}

		page = FetchPage{
			Messages:   messages,
			NextCursor: nextCursor,
		}
		return nil
	})
	if err != nil {
		return FetchPage{}, err
	}

	return page, nil
}

func (c *MTProtoCollector) FetchMessages(ctx context.Context, channel ChannelRef, messageIDs []int64) ([]Message, error) {
	ids := sanitizeMessageIDs(messageIDs)
	if len(ids) == 0 {
		return []Message{}, nil
	}

	var out []Message
	err := c.withAPI(ctx, func(ctx context.Context, api *tg.Client) error {
		inputPeer, _, err := c.resolveInputPeer(ctx, api, channel)
		if err != nil {
			return err
		}

		inputIDs := make([]tg.InputMessageClass, 0, len(ids))
		for _, messageID := range ids {
			inputIDs = append(inputIDs, &tg.InputMessageID{ID: clampTLInt(messageID)})
		}

		res, err := api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{
				ChannelID:  inputPeer.ChannelID,
				AccessHash: inputPeer.AccessHash,
			},
			ID: inputIDs,
		})
		if err != nil {
			return fmt.Errorf("channels.getMessages: %w", err)
		}

		modified, ok := res.AsModified()
		if !ok {
			return errors.New("channels.getMessages returned an unmodified response")
		}

		classes := modified.GetMessages()
		byID := make(map[int64]Message, len(classes))
		missing := make([]string, 0)

		for _, class := range classes {
			switch msg := class.(type) {
			case *tg.Message:
				byID[int64(msg.GetID())] = mapTGMessage(msg)
			case *tg.MessageService:
				byID[int64(msg.GetID())] = mapTGServiceMessage(msg)
			case *tg.MessageEmpty:
				missing = append(missing, itoa64(int64(msg.GetID())))
			}
		}

		ordered := make([]Message, 0, len(ids))
		for _, messageID := range ids {
			msg, ok := byID[messageID]
			if !ok {
				missing = append(missing, itoa64(messageID))
				continue
			}
			ordered = append(ordered, msg)
		}

		if len(missing) > 0 {
			return fmt.Errorf("messages not found or inaccessible: %s", strings.Join(uniqueStrings(missing), ", "))
		}

		out = ordered
		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

func (c *MTProtoCollector) withAPI(ctx context.Context, fn func(ctx context.Context, api *tg.Client) error) error {
	apiID, err := strconv.Atoi(strings.TrimSpace(c.APIID))
	if err != nil {
		return fmt.Errorf("invalid TELEGRAM_API_ID: %w", err)
	}
	if apiID <= 0 {
		return errors.New("TELEGRAM_API_ID must be > 0")
	}
	apiHash := strings.TrimSpace(c.APIHash)
	if apiHash == "" {
		return errors.New("TELEGRAM_API_HASH is required for mtproto mode")
	}

	sessionFile := strings.TrimSpace(c.SessionFile)
	if sessionFile == "" {
		sessionFile = "./data/tg.session.json"
	}
	if err := ensureDir(filepath.Dir(sessionFile)); err != nil {
		return fmt.Errorf("prepare session dir: %w", err)
	}

	client := td.NewClient(apiID, apiHash, td.Options{
		SessionStorage: &session.FileStorage{Path: sessionFile},
	})

	return client.Run(ctx, func(ctx context.Context) error {
		if err := c.ensureAuthorized(ctx, client); err != nil {
			return err
		}
		return fn(ctx, client.API())
	})
}

func (c *MTProtoCollector) ensureAuthorized(ctx context.Context, client *td.Client) error {
	status, err := client.Auth().Status(ctx)
	if err != nil {
		return fmt.Errorf("check auth status: %w", err)
	}
	if status.Authorized {
		return nil
	}

	phone := strings.TrimSpace(c.Phone)
	if phone == "" {
		return errors.New("telegram session is not authorized; set TELEGRAM_PHONE")
	}

	password, err := c.resolvePassword()
	if err != nil {
		return err
	}

	flow := tdauth.NewFlow(
		tdauth.Constant(phone, password, tdauth.CodeAuthenticatorFunc(func(ctx context.Context, _ *tg.AuthSentCode) (string, error) {
			// Ask for code only when Telegram has actually sent it.
			return c.resolveAuthCode()
		})),
		tdauth.SendCodeOptions{},
	)
	for attempt := 1; attempt <= 2; attempt++ {
		if err := client.Auth().IfNecessary(ctx, flow); err != nil {
			if attempt < 2 && isAuthRestartError(err) {
				continue
			}
			return fmt.Errorf("run auth flow: %w", err)
		}
		return nil
	}
	return errors.New("run auth flow: unexpected auth retry state")
}

func (c *MTProtoCollector) resolveAuthCode() (string, error) {
	if code := strings.TrimSpace(c.AuthCode); code != "" {
		return code, nil
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("telegram session is not authorized; set TELEGRAM_AUTH_CODE for first login")
	}

	fmt.Print("Enter Telegram auth code: ")
	reader := bufio.NewReader(os.Stdin)
	code, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read telegram auth code: %w", err)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return "", errors.New("telegram auth code is empty")
	}
	return code, nil
}

func (c *MTProtoCollector) resolvePassword() (string, error) {
	if password := strings.TrimSpace(c.Password); password != "" {
		return password, nil
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", nil
	}

	fmt.Print("Enter Telegram 2FA password (press Enter if disabled): ")
	passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read telegram 2FA password: %w", err)
	}
	return strings.TrimSpace(string(passwordBytes)), nil
}

func isAuthRestartError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToUpper(err.Error()), "AUTH_RESTART")
}

func (c *MTProtoCollector) resolveInputPeer(ctx context.Context, api *tg.Client, channel ChannelRef) (*tg.InputPeerChannel, ChannelRef, error) {
	if channel.ID != nil && channel.AccessHash != nil {
		return &tg.InputPeerChannel{
			ChannelID:  *channel.ID,
			AccessHash: *channel.AccessHash,
		}, channel, nil
	}

	username := strings.TrimSpace(channel.Username)
	sourceURL := strings.TrimSpace(channel.URL)
	if username == "" {
		resolvedUsername, resolvedURL, err := ResolveUsername(ResolveInput{
			URL:      sourceURL,
			Username: username,
		})
		if err != nil {
			return nil, ChannelRef{}, fmt.Errorf("resolve source username: %w", err)
		}
		username = resolvedUsername
		sourceURL = resolvedURL
	}

	resolvedChannel, err := c.resolveChannelByUsername(ctx, api, username, sourceURL)
	if err != nil {
		return nil, ChannelRef{}, err
	}
	if resolvedChannel.ID == nil || resolvedChannel.AccessHash == nil {
		return nil, ChannelRef{}, errors.New("resolved channel has no access hash")
	}

	return &tg.InputPeerChannel{
		ChannelID:  *resolvedChannel.ID,
		AccessHash: *resolvedChannel.AccessHash,
	}, resolvedChannel, nil
}

func (c *MTProtoCollector) resolveChannelByUsername(ctx context.Context, api *tg.Client, username, sourceURL string) (ChannelRef, error) {
	username = normalizeUsername(username)
	if username == "" {
		return ChannelRef{}, errors.New("username is required")
	}
	if strings.TrimSpace(sourceURL) == "" {
		sourceURL = "https://t.me/" + username
	}

	resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
		Username: username,
	})
	if err != nil {
		return ChannelRef{}, fmt.Errorf("contacts.resolveUsername @%s: %w", username, err)
	}

	peer, ok := resolved.Peer.(*tg.PeerChannel)
	if !ok {
		return ChannelRef{}, fmt.Errorf("resolved peer is %T, expected channel", resolved.Peer)
	}

	channelID := peer.ChannelID
	out := ChannelRef{
		ID:       &channelID,
		Username: username,
		Title:    "@" + username,
		URL:      sourceURL,
	}

	for _, chat := range resolved.Chats {
		switch value := chat.(type) {
		case *tg.Channel:
			if value.GetID() != channelID {
				continue
			}
			if accessHash, ok := value.GetAccessHash(); ok {
				hash := accessHash
				out.AccessHash = &hash
			}
			if title := strings.TrimSpace(value.GetTitle()); title != "" {
				out.Title = title
			}
			if resolvedUsername, ok := value.GetUsername(); ok && strings.TrimSpace(resolvedUsername) != "" {
				out.Username = normalizeUsername(resolvedUsername)
				out.URL = "https://t.me/" + out.Username
			}
		case *tg.ChannelForbidden:
			if value.GetID() != channelID {
				continue
			}
			hash := value.GetAccessHash()
			out.AccessHash = &hash
			if title := strings.TrimSpace(value.GetTitle()); title != "" {
				out.Title = title
			}
		}
	}

	if out.AccessHash == nil {
		return ChannelRef{}, fmt.Errorf("unable to resolve access hash for @%s", username)
	}
	return out, nil
}

func (c *MTProtoCollector) resolveChannelByID(ctx context.Context, api *tg.Client, channelID int64) (ChannelRef, error) {
	const stopIteration = "telegram_resolve_channel_found"

	var out ChannelRef
	err := query.GetDialogs(api).BatchSize(100).ForEach(ctx, func(ctx context.Context, elem querydialogs.Elem) error {
		inputPeer, ok := elem.Peer.(*tg.InputPeerChannel)
		if !ok || inputPeer.ChannelID != channelID {
			return nil
		}

		out = ChannelRef{
			ID:         &inputPeer.ChannelID,
			AccessHash: &inputPeer.AccessHash,
			URL:        PrivateMessageURL(channelID, 1),
			Title:      "channel:" + itoa64(channelID),
		}

		if channel, ok := elem.Entities.Channel(channelID); ok {
			if title := strings.TrimSpace(channel.GetTitle()); title != "" {
				out.Title = title
			}
			if username, ok := channel.GetUsername(); ok && strings.TrimSpace(username) != "" {
				out.Username = normalizeUsername(username)
				out.URL = "https://t.me/" + out.Username
			}
		}

		return errors.New(stopIteration)
	})
	if err != nil && err.Error() != stopIteration {
		return ChannelRef{}, fmt.Errorf("scan dialogs for channel %d: %w", channelID, err)
	}
	if out.ID == nil || out.AccessHash == nil {
		return ChannelRef{}, fmt.Errorf("channel %d was not found in accessible dialogs", channelID)
	}
	return out, nil
}

func (c *MTProtoCollector) resolveChannelByInviteHash(ctx context.Context, api *tg.Client, inviteHash, sourceURL string) (ChannelRef, error) {
	inviteHash = strings.TrimSpace(inviteHash)
	if inviteHash == "" {
		return ChannelRef{}, errors.New("invite hash is required")
	}
	if strings.TrimSpace(sourceURL) == "" {
		sourceURL = "https://t.me/+" + inviteHash
	}

	invite, err := api.MessagesCheckChatInvite(ctx, inviteHash)
	if err != nil {
		return ChannelRef{}, fmt.Errorf("messages.checkChatInvite %s: %w", inviteHash, err)
	}

	already, ok := invite.(*tg.ChatInviteAlready)
	if !ok {
		return ChannelRef{}, errors.New("invite link is valid, but this Telegram account has not joined the channel; join it with the configured account first")
	}

	out := ChannelRef{URL: sourceURL}
	switch chat := already.GetChat().(type) {
	case *tg.Channel:
		channelID := chat.GetID()
		out.ID = &channelID
		if accessHash, ok := chat.GetAccessHash(); ok {
			hash := accessHash
			out.AccessHash = &hash
		}
		if title := strings.TrimSpace(chat.GetTitle()); title != "" {
			out.Title = title
		}
		if username, ok := chat.GetUsername(); ok && strings.TrimSpace(username) != "" {
			out.Username = normalizeUsername(username)
		}
	case *tg.ChannelForbidden:
		channelID := chat.GetID()
		hash := chat.GetAccessHash()
		out.ID = &channelID
		out.AccessHash = &hash
		if title := strings.TrimSpace(chat.GetTitle()); title != "" {
			out.Title = title
		}
	default:
		return ChannelRef{}, fmt.Errorf("invite does not point to a channel: %T", already.GetChat())
	}

	if out.ID == nil || out.AccessHash == nil {
		return ChannelRef{}, errors.New("unable to resolve access hash from invite link")
	}
	if out.Username != "" {
		out.URL = "https://t.me/" + out.Username
	}
	return out, nil
}

func mapTGMessage(msg *tg.Message) Message {
	var groupedID *int64
	if value, ok := msg.GetGroupedID(); ok {
		v := value
		groupedID = &v
	}

	var postedAt *time.Time
	if msg.Date > 0 {
		value := time.Unix(int64(msg.Date), 0).UTC()
		postedAt = &value
	}

	var editedAt *time.Time
	if value, ok := msg.GetEditDate(); ok && value > 0 {
		v := time.Unix(int64(value), 0).UTC()
		editedAt = &v
	}

	var viewsCount *int
	if value, ok := msg.GetViews(); ok {
		v := value
		viewsCount = &v
	}

	var forwardsCount *int
	if value, ok := msg.GetForwards(); ok {
		v := value
		forwardsCount = &v
	}

	var mediaJSON map[string]any
	hasMedia := false
	if media, ok := msg.GetMedia(); ok && media != nil {
		hasMedia = true
		mediaJSON = map[string]any{
			"type": media.TypeName(),
		}
	}

	text := strings.TrimSpace(msg.GetMessage())
	textRaw := text
	captionRaw := ""
	if hasMedia {
		textRaw = ""
		captionRaw = text
	}

	raw := map[string]any{
		"type": "message",
		"id":   msg.GetID(),
		"date": msg.GetDate(),
		"post": msg.Post,
	}
	if peer := peerToString(msg.GetPeerID()); peer != "" {
		raw["peer"] = peer
	}
	if fromPeer, ok := msg.GetFromID(); ok {
		if peer := peerToString(fromPeer); peer != "" {
			raw["from_peer"] = peer
		}
	}

	return Message{
		MessageID:        int64(msg.GetID()),
		GroupedID:        groupedID,
		PostedAt:         postedAt,
		EditedAt:         editedAt,
		TextRaw:          textRaw,
		CaptionRaw:       captionRaw,
		RawJSON:          raw,
		MediaJSON:        mediaJSON,
		EntitiesJSON:     extractEntities(msg),
		ViewsCount:       viewsCount,
		ForwardsCount:    forwardsCount,
		ReplyToMessageID: extractReplyToMessageID(msg),
		ForwardFromName:  extractForwardFromName(msg),
		ForwardFromChat:  extractForwardFromChat(msg),
		HasMedia:         hasMedia,
	}
}

func mapTGServiceMessage(msg *tg.MessageService) Message {
	var postedAt *time.Time
	if msg.Date > 0 {
		value := time.Unix(int64(msg.Date), 0).UTC()
		postedAt = &value
	}

	raw := map[string]any{
		"type": "service",
		"id":   msg.GetID(),
		"date": msg.GetDate(),
	}
	if msg.Action != nil {
		raw["action_type"] = msg.Action.TypeName()
	}

	return Message{
		MessageID:        int64(msg.GetID()),
		PostedAt:         postedAt,
		TextRaw:          "",
		CaptionRaw:       "",
		RawJSON:          raw,
		ReplyToMessageID: extractServiceReplyToMessageID(msg),
		HasMedia:         false,
	}
}

func extractEntities(msg *tg.Message) map[string]any {
	entities, ok := msg.GetEntities()
	if !ok || len(entities) == 0 {
		return nil
	}

	items := make([]map[string]any, 0, len(entities))
	for _, entity := range entities {
		if entity == nil {
			continue
		}
		items = append(items, map[string]any{
			"type":   entity.TypeName(),
			"offset": entity.GetOffset(),
			"length": entity.GetLength(),
		})
	}

	if len(items) == 0 {
		return nil
	}
	return map[string]any{"items": items}
}

func extractReplyToMessageID(msg *tg.Message) *int64 {
	reply, ok := msg.GetReplyTo()
	if !ok {
		return nil
	}
	header, ok := reply.(*tg.MessageReplyHeader)
	if !ok {
		return nil
	}
	replyID, ok := header.GetReplyToMsgID()
	if !ok {
		return nil
	}
	value := int64(replyID)
	return &value
}

func extractServiceReplyToMessageID(msg *tg.MessageService) *int64 {
	reply, ok := msg.GetReplyTo()
	if !ok {
		return nil
	}
	header, ok := reply.(*tg.MessageReplyHeader)
	if !ok {
		return nil
	}
	replyID, ok := header.GetReplyToMsgID()
	if !ok {
		return nil
	}
	value := int64(replyID)
	return &value
}

func extractForwardFromName(msg *tg.Message) *string {
	forward, ok := msg.GetFwdFrom()
	if !ok {
		return nil
	}

	if value, ok := forward.GetFromName(); ok && strings.TrimSpace(value) != "" {
		trimmed := strings.TrimSpace(value)
		return &trimmed
	}
	if value, ok := forward.GetPostAuthor(); ok && strings.TrimSpace(value) != "" {
		trimmed := strings.TrimSpace(value)
		return &trimmed
	}
	return nil
}

func extractForwardFromChat(msg *tg.Message) *string {
	forward, ok := msg.GetFwdFrom()
	if !ok {
		return nil
	}

	if peer, ok := forward.GetSavedFromPeer(); ok {
		if value := peerToString(peer); value != "" {
			return &value
		}
	}
	if peer, ok := forward.GetFromID(); ok {
		if value := peerToString(peer); value != "" {
			return &value
		}
	}
	return nil
}

func peerToString(peer tg.PeerClass) string {
	switch value := peer.(type) {
	case *tg.PeerChannel:
		return "channel:" + itoa64(value.ChannelID)
	case *tg.PeerChat:
		return "chat:" + itoa64(value.ChatID)
	case *tg.PeerUser:
		return "user:" + itoa64(value.UserID)
	default:
		if peer == nil {
			return ""
		}
		return peer.TypeName()
	}
}

func ensureDir(path string) error {
	if strings.TrimSpace(path) == "" || path == "." {
		return nil
	}
	return os.MkdirAll(path, 0o700)
}

func clampTLInt(value int64) int {
	const max = int64(2147483647)
	if value > max {
		return int(max)
	}
	if value < 0 {
		return 0
	}
	return int(value)
}

func sanitizeMessageIDs(messageIDs []int64) []int64 {
	seen := make(map[int64]struct{}, len(messageIDs))
	out := make([]int64, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		if messageID <= 0 {
			continue
		}
		if _, ok := seen[messageID]; ok {
			continue
		}
		seen[messageID] = struct{}{}
		out = append(out, messageID)
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

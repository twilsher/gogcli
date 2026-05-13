package cmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/api/gmail/v1"

	"github.com/steipete/gogcli/internal/ui"
)

var gmailUnsubscribeHTTPClient = http.DefaultClient

type GmailUnsubCmd struct {
	MessageIDs []string `arg:"" name:"messageId" help:"Message IDs to unsubscribe"`
}

type gmailUnsubscribeResult struct {
	messageID string
	status    string
	method    string
	link      string
	code      int
}

func (c *GmailUnsubCmd) Run(ctx context.Context, flags *RootFlags) error {
	u := ui.FromContext(ctx)
	ids := make([]string, 0, len(c.MessageIDs))
	for _, id := range c.MessageIDs {
		if normalized := normalizeGmailMessageID(id); normalized != "" {
			ids = append(ids, normalized)
		}
	}
	if len(ids) == 0 {
		return usage("missing messageId")
	}

	if dryRunErr := dryRunExit(ctx, flags, "gmail.unsubscribe", map[string]any{
		"message_ids": ids,
	}); dryRunErr != nil {
		return dryRunErr
	}

	account, svc, err := requireGmailService(ctx, flags)
	if err != nil {
		return err
	}

	var failed []gmailUnsubscribeResult
	for _, id := range ids {
		result := unsubscribeGmailMessage(ctx, account, svc, id)
		printGmailUnsubscribeResult(u, result)
		if result.status != "ok" {
			failed = append(failed, result)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("unsubscribe failed for %d message%s", len(failed), pluralS(len(failed)))
	}
	return nil
}

func unsubscribeGmailMessage(ctx context.Context, account string, svc *gmail.Service, messageID string) gmailUnsubscribeResult {
	result := gmailUnsubscribeResult{messageID: messageID}
	msg, err := svc.Users.Messages.Get("me", messageID).
		Format(gmailFormatMetadata).
		MetadataHeaders("List-Unsubscribe", "List-Unsubscribe-Post").
		Fields("id,payload/headers").
		Context(ctx).
		Do()
	if err != nil {
		result.status = "failed"
		return result
	}

	header := headerValue(msg.Payload, "List-Unsubscribe")
	if strings.TrimSpace(header) == "" {
		result.status = "noheader"
		return result
	}

	links := parseListUnsubscribe(header)
	httpsLink, mailtoLink := selectUnsubscribeLinks(links)
	if httpsLink != "" {
		return unsubscribeViaHTTPS(ctx, messageID, httpsLink, headerValue(msg.Payload, "List-Unsubscribe-Post"))
	}
	if mailtoLink != "" {
		return unsubscribeViaMailto(ctx, account, svc, messageID, mailtoLink)
	}

	result.status = "failed"
	return result
}

func selectUnsubscribeLinks(links []string) (httpsLink string, mailtoLink string) {
	for _, link := range links {
		lower := strings.ToLower(strings.TrimSpace(link))
		switch {
		case strings.HasPrefix(lower, "https://") && httpsLink == "":
			httpsLink = link
		case strings.HasPrefix(lower, "mailto:") && mailtoLink == "":
			mailtoLink = link
		}
	}
	return httpsLink, mailtoLink
}

func unsubscribeViaHTTPS(ctx context.Context, messageID string, link string, postHeader string) gmailUnsubscribeResult {
	result := gmailUnsubscribeResult{messageID: messageID, link: link}
	code, err := doUnsubscribeHTTPRequest(ctx, http.MethodPost, link, postHeader)
	result.method = http.MethodPost
	result.code = code
	if err == nil && code >= 200 && code < 400 {
		result.status = "ok"
		return result
	}
	if code >= 400 && code < 500 {
		code, err = doUnsubscribeHTTPRequest(ctx, http.MethodGet, link, "")
		result.method = http.MethodGet
		result.code = code
		if err == nil && code >= 200 && code < 400 {
			result.status = "ok"
			return result
		}
	}
	result.status = "failed"
	return result
}

func doUnsubscribeHTTPRequest(ctx context.Context, method string, link string, postHeader string) (int, error) {
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader("List-Unsubscribe=One-Click")
	}
	req, err := http.NewRequestWithContext(ctx, method, link, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "gogcli")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if strings.TrimSpace(postHeader) != "" {
			req.Header.Set("List-Unsubscribe-Post", strings.TrimSpace(postHeader))
		}
	}
	resp, err := gmailUnsubscribeHTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func unsubscribeViaMailto(ctx context.Context, account string, svc *gmail.Service, messageID string, link string) gmailUnsubscribeResult {
	result := gmailUnsubscribeResult{
		messageID: messageID,
		method:    "mailto",
		link:      link,
	}
	if err := checkAccountNoSend(account); err != nil {
		result.status = "failed"
		return result
	}

	to, subject, err := parseUnsubscribeMailto(link)
	if err != nil {
		result.status = "failed"
		return result
	}

	raw, err := buildRFC822(mailOptions{
		From:    account,
		To:      []string{to},
		Subject: subject,
		Body:    "",
	}, nil)
	if err != nil {
		result.status = "failed"
		return result
	}
	_, err = svc.Users.Messages.Send("me", &gmail.Message{
		Raw: base64.RawURLEncoding.EncodeToString(raw),
	}).Context(ctx).Do()
	if err != nil {
		result.status = "failed"
		return result
	}
	result.status = "ok"
	return result
}

func parseUnsubscribeMailto(link string) (to string, subject string, err error) {
	u, err := url.Parse(link)
	if err != nil {
		return "", "", err
	}
	if !strings.EqualFold(u.Scheme, "mailto") {
		return "", "", fmt.Errorf("not a mailto URL: %s", link)
	}
	to = strings.TrimSpace(u.Opaque)
	if to == "" {
		to = strings.TrimSpace(u.Path)
	}
	if idx := strings.IndexByte(to, '?'); idx >= 0 {
		to = to[:idx]
	}
	if decoded, decodeErr := url.PathUnescape(to); decodeErr == nil {
		to = decoded
	}
	if strings.TrimSpace(to) == "" {
		return "", "", errors.New("empty mailto recipient")
	}
	values := u.Query()
	subject = strings.TrimSpace(values.Get("subject"))
	if subject == "" {
		subject = "unsubscribe"
	}
	return to, subject, nil
}

func printGmailUnsubscribeResult(u *ui.UI, result gmailUnsubscribeResult) {
	switch result.status {
	case "ok":
		u.Out().Printf("ok %s %s %s", result.messageID, result.method, result.link)
	case "noheader":
		u.Out().Printf("noheader %s", result.messageID)
	default:
		u.Out().Printf("failed %s %d", result.messageID, result.code)
	}
}

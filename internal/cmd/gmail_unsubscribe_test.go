package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steipete/gogcli/internal/ui"
)

func TestGmailUnsubCmd_HTTPSPost(t *testing.T) {
	var methods []string
	unsubSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected unsubscribe method: %s", r.Method)
		}
		if got := r.Header.Get("List-Unsubscribe-Post"); got != "List-Unsubscribe=One-Click" {
			t.Fatalf("unexpected List-Unsubscribe-Post header: %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer unsubSrv.Close()
	stubUnsubscribeHTTPClient(t, unsubSrv.Client())

	svc, cleanup := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/gmail/v1/users/me/messages/m1") {
			writeGmailMessageWithHeaders(t, w, "m1", map[string]string{
				"List-Unsubscribe":      "<" + unsubSrv.URL + "/one-click>",
				"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
			})
			return
		}
		http.NotFound(w, r)
	})
	defer cleanup()
	stubGmailServiceForTest(t, svc)

	out, err := runGmailUnsubForTest(t, []string{"m1"})
	if err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if got, want := strings.TrimSpace(out), "ok m1 POST "+unsubSrv.URL+"/one-click"; got != want {
		t.Fatalf("unexpected output:\n got: %q\nwant: %q", got, want)
	}
	if len(methods) != 1 || methods[0] != http.MethodPost {
		t.Fatalf("unexpected methods: %v", methods)
	}
}

func TestGmailUnsubCmd_HTTPSPost4xxFallsBackToGET(t *testing.T) {
	var methods []string
	unsubSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected unsubscribe method: %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer unsubSrv.Close()
	stubUnsubscribeHTTPClient(t, unsubSrv.Client())

	svc, cleanup := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/gmail/v1/users/me/messages/m1") {
			writeGmailMessageWithHeaders(t, w, "m1", map[string]string{
				"List-Unsubscribe": "<" + unsubSrv.URL + "/fallback>",
			})
			return
		}
		http.NotFound(w, r)
	})
	defer cleanup()
	stubGmailServiceForTest(t, svc)

	out, err := runGmailUnsubForTest(t, []string{"m1"})
	if err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if got, want := strings.TrimSpace(out), "ok m1 GET "+unsubSrv.URL+"/fallback"; got != want {
		t.Fatalf("unexpected output:\n got: %q\nwant: %q", got, want)
	}
	if got, want := strings.Join(methods, ","), "POST,GET"; got != want {
		t.Fatalf("unexpected methods: %s", got)
	}
}

func TestGmailUnsubCmd_NoHeaderReturnsNonZero(t *testing.T) {
	svc, cleanup := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/gmail/v1/users/me/messages/m1") {
			writeGmailMessageWithHeaders(t, w, "m1", nil)
			return
		}
		http.NotFound(w, r)
	})
	defer cleanup()
	stubGmailServiceForTest(t, svc)

	out, err := runGmailUnsubForTest(t, []string{"m1"})
	if err == nil {
		t.Fatal("expected noheader to return an error")
	}
	if got, want := strings.TrimSpace(out), "noheader m1"; got != want {
		t.Fatalf("unexpected output:\n got: %q\nwant: %q", got, want)
	}
}

func TestGmailUnsubCmd_MailtoSendsEmptyMessage(t *testing.T) {
	var rawSent string
	svc, cleanup := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/gmail/v1/users/me/messages/m1"):
			writeGmailMessageWithHeaders(t, w, "m1", map[string]string{
				"List-Unsubscribe": "<mailto:unsubscribe@example.com?subject=remove>",
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/gmail/v1/users/me/messages/send"):
			var req struct {
				Raw string `json:"raw"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode send request: %v", err)
			}
			rawSent = decodeRawGmailForTest(t, req.Raw)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "sent1", "threadId": "thread1"})
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()
	stubGmailServiceForTest(t, svc)

	out, err := runGmailUnsubForTest(t, []string{"m1"})
	if err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if got, want := strings.TrimSpace(out), "ok m1 mailto mailto:unsubscribe@example.com?subject=remove"; got != want {
		t.Fatalf("unexpected output:\n got: %q\nwant: %q", got, want)
	}
	if !strings.Contains(rawSent, "To: unsubscribe@example.com\n") {
		t.Fatalf("missing To in sent message:\n%s", rawSent)
	}
	if !strings.Contains(rawSent, "Subject: remove\n") {
		t.Fatalf("missing Subject in sent message:\n%s", rawSent)
	}
	if !strings.HasSuffix(rawSent, "\n\n") {
		t.Fatalf("expected empty body, got:\n%s", rawSent)
	}
}

func runGmailUnsubForTest(t *testing.T, args []string) (string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	u, err := ui.New(ui.Options{Stdout: &stdout, Stderr: &stderr, Color: "never"})
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}
	ctx := ui.WithUI(context.Background(), u)
	err = runKong(t, &GmailUnsubCmd{}, args, ctx, &RootFlags{Account: "me@example.com"})
	return stdout.String(), err
}

func writeGmailMessageWithHeaders(t *testing.T, w http.ResponseWriter, id string, headers map[string]string) {
	t.Helper()

	items := make([]map[string]string, 0, len(headers))
	for name, value := range headers {
		items = append(items, map[string]string{"name": name, "value": value})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": id,
		"payload": map[string]any{
			"headers": items,
		},
	})
}

func stubUnsubscribeHTTPClient(t *testing.T, client *http.Client) {
	t.Helper()

	orig := gmailUnsubscribeHTTPClient
	t.Cleanup(func() { gmailUnsubscribeHTTPClient = orig })
	gmailUnsubscribeHTTPClient = client
}

func decodeRawGmailForTest(t *testing.T, raw string) string {
	t.Helper()

	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

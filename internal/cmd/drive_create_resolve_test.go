package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"github.com/steipete/gogcli/internal/outfmt"
	"github.com/steipete/gogcli/internal/ui"
)

func makeDriveTestService(t *testing.T, handler http.HandlerFunc) (*drive.Service, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	svc, err := drive.NewService(context.Background(),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
		option.WithEndpoint(srv.URL+"/"),
	)
	if err != nil {
		srv.Close()
		t.Fatalf("NewService: %v", err)
	}
	return svc, srv.Close
}

func makeTestContext(t *testing.T, json bool) (context.Context, *strings.Builder) {
	t.Helper()
	var buf strings.Builder
	u, err := ui.New(ui.Options{Stdout: io.Discard, Stderr: io.Discard, Color: "never"})
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}
	ctx := ui.WithUI(context.Background(), u)
	ctx = outfmt.WithMode(ctx, outfmt.Mode{JSON: json})
	return ctx, &buf
}

// TestDriveCreateCmd_Doc verifies that drive create posts to /files with the correct mimeType.
func TestDriveCreateCmd_Doc(t *testing.T) {
	origNew := newDriveService
	t.Cleanup(func() { newDriveService = origNew })

	svc, closeSrv := makeDriveTestService(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/drive/v3")
		if r.Method != http.MethodPost || path != "/files" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["name"] != "My Doc" {
			t.Fatalf("expected name 'My Doc', got %v", payload["name"])
		}
		if payload["mimeType"] != driveMimeGoogleDoc {
			t.Fatalf("expected mimeType %q, got %v", driveMimeGoogleDoc, payload["mimeType"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "doc1",
			"name":        "My Doc",
			"mimeType":    driveMimeGoogleDoc,
			"webViewLink": "https://docs.google.com/document/d/doc1/edit",
		})
	})
	defer closeSrv()
	newDriveService = func(context.Context, string) (*drive.Service, error) { return svc, nil }

	ctx, _ := makeTestContext(t, true)
	flags := &RootFlags{Account: "a@b.com"}

	out := captureStdout(t, func() {
		cmd := &DriveCreateCmd{}
		if err := runKong(t, cmd, []string{"My Doc"}, ctx, flags); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})

	var parsed struct {
		File *drive.File `json:"file"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json parse: %v out=%q", err, out)
	}
	if parsed.File == nil || parsed.File.Id != "doc1" {
		t.Fatalf("unexpected file: %#v", parsed.File)
	}
}

// TestDriveCreateCmd_Sheet verifies --type sheet sends the sheet mimeType.
func TestDriveCreateCmd_Sheet(t *testing.T) {
	origNew := newDriveService
	t.Cleanup(func() { newDriveService = origNew })

	svc, closeSrv := makeDriveTestService(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/drive/v3")
		if r.Method != http.MethodPost || path != "/files" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["mimeType"] != driveMimeGoogleSheet {
			t.Fatalf("expected sheet mimeType, got %v", payload["mimeType"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "sheet1", "name": "My Sheet", "mimeType": driveMimeGoogleSheet,
		})
	})
	defer closeSrv()
	newDriveService = func(context.Context, string) (*drive.Service, error) { return svc, nil }

	ctx, _ := makeTestContext(t, true)
	flags := &RootFlags{Account: "a@b.com"}

	out := captureStdout(t, func() {
		cmd := &DriveCreateCmd{}
		if err := runKong(t, cmd, []string{"--type", "sheet", "My Sheet"}, ctx, flags); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})
	var parsed struct {
		File *drive.File `json:"file"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if parsed.File == nil || parsed.File.Id != "sheet1" {
		t.Fatalf("unexpected: %#v", parsed.File)
	}
}

// TestDriveCreateCmd_InvalidType verifies that an unknown --type returns a usage error.
func TestDriveCreateCmd_InvalidType(t *testing.T) {
	origNew := newDriveService
	t.Cleanup(func() { newDriveService = origNew })
	newDriveService = func(context.Context, string) (*drive.Service, error) {
		t.Fatal("should not reach API for invalid type")
		return nil, nil
	}

	ctx, _ := makeTestContext(t, false)
	flags := &RootFlags{Account: "a@b.com"}
	cmd := &DriveCreateCmd{}
	err := runKong(t, cmd, []string{"--type", "bogus", "My File"}, ctx, flags)
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("expected usage error for invalid type, got: %v", err)
	}
}

// TestDriveCommentsResolveCmd verifies resolve posts a reply with action="resolve".
func TestDriveCommentsResolveCmd(t *testing.T) {
	origNew := newDriveService
	t.Cleanup(func() { newDriveService = origNew })

	svc, closeSrv := makeDriveTestService(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/drive/v3")
		if r.Method != http.MethodPost || path != "/files/fid1/comments/cid1/replies" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["action"] != "resolve" {
			t.Fatalf("expected action=resolve, got %v", payload["action"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "rid1", "action": "resolve", "createdTime": "2026-04-23T09:00:00Z",
		})
	})
	defer closeSrv()
	newDriveService = func(context.Context, string) (*drive.Service, error) { return svc, nil }

	ctx, _ := makeTestContext(t, true)
	flags := &RootFlags{Account: "a@b.com"}

	out := captureStdout(t, func() {
		cmd := &DriveCommentsResolveCmd{}
		if err := runKong(t, cmd, []string{"fid1", "cid1"}, ctx, flags); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json parse: %v out=%q", err, out)
	}
	if parsed["resolved"] != true {
		t.Fatalf("expected resolved=true, got %v", parsed["resolved"])
	}
	if parsed["fileId"] != "fid1" || parsed["commentId"] != "cid1" {
		t.Fatalf("unexpected ids: %v", parsed)
	}
}

// TestDriveCommentsUnresolveCmd verifies unresolve posts a reply with action="reopen".
func TestDriveCommentsUnresolveCmd(t *testing.T) {
	origNew := newDriveService
	t.Cleanup(func() { newDriveService = origNew })

	svc, closeSrv := makeDriveTestService(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/drive/v3")
		if r.Method != http.MethodPost || path != "/files/fid1/comments/cid1/replies" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["action"] != "reopen" {
			t.Fatalf("expected action=reopen, got %v", payload["action"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "rid2", "action": "reopen", "createdTime": "2026-04-23T09:01:00Z",
		})
	})
	defer closeSrv()
	newDriveService = func(context.Context, string) (*drive.Service, error) { return svc, nil }

	ctx, _ := makeTestContext(t, true)
	flags := &RootFlags{Account: "a@b.com"}

	out := captureStdout(t, func() {
		cmd := &DriveCommentsUnresolveCmd{}
		if err := runKong(t, cmd, []string{"fid1", "cid1"}, ctx, flags); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json parse: %v out=%q", err, out)
	}
	if parsed["reopened"] != true {
		t.Fatalf("expected reopened=true, got %v", parsed["reopened"])
	}
	if parsed["fileId"] != "fid1" || parsed["commentId"] != "cid1" {
		t.Fatalf("unexpected ids: %v", parsed)
	}
}

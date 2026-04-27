## Summary

Adds three new commands to `gog drive` and closes a symmetry gap in `gog docs comments`:

- `gog drive create <name> [--type doc|sheet|slides] [--parent <folderId>]`
- `gog drive comments resolve <fileId> <commentId> [-m message]`
- `gog drive comments unresolve <fileId> <commentId> [-m message]` (alias: `reopen`)
- `gog docs comments unresolve <docId> <commentId> [-m message]` (alias: `reopen`)

`gog docs comments` already had `resolve`; `drive` had neither, and neither surface had `unresolve`.

## What's added

### `gog drive create`

Creates a blank native Google Workspace file without uploading content.

```bash
gog drive create "Meeting Notes" --account me@example.com
gog drive create "Q1 Budget" --type sheet --parent <folderId>
gog drive create "Product Deck" --type slides
```

Uses `files.create` with the appropriate native mimeType (`doc` → `application/vnd.google-apps.document`, etc.). Supports `--dry-run` and `--json`.

### `gog drive comments resolve` / `unresolve`

```bash
gog drive comments resolve   <fileId> <commentId>
gog drive comments unresolve <fileId> <commentId>   # alias: reopen
gog drive comments resolve   <fileId> <commentId> -m "Fixed in abc123"
```

**API note:** `resolved` is read-only on the Comment resource — it cannot be PATCHed directly. Both operations create a reply with `action: "resolve"` or `action: "reopen"`. The value `"unresolve"` returns a 400; `"reopen"` is correct. Verified against the live API.

### `gog docs comments unresolve`

Same as `drive comments unresolve` but takes a `docId`. Completes the symmetry with `gog docs comments resolve`.

## Files changed

- `internal/cmd/drive.go` — `DriveCreateCmd` + `driveCreateMimeType`; registered in `DriveCmd`
- `internal/cmd/drive_comments.go` — `DriveCommentsResolveCmd`, `DriveCommentsUnresolveCmd`; registered in `DriveCommentsCmd`
- `internal/cmd/docs_comments.go` — `DocsCommentsUnresolveCmd`; registered in `DocsCommentsCmd`
- `internal/cmd/comment_ops.go` — `unresolveDriveComment` (action: `"reopen"`)
- `internal/cmd/drive_create_resolve_test.go` — 5 new unit tests

## Validation

- `go build ./...` clean
- `go test ./internal/cmd/... -run "TestDriveCreate|TestDriveCommentsResolve|TestDriveCommentsUnresolve"` — 5/5 pass
- Live API: created a doc, added a comment, resolved (verified `resolved: true`), reopened (verified `resolved: false`), trashed doc

## Prompt Used

```text
Implement comment resolution in the gog CLI (Drive comments). The repo is at
~/dev/gogcli. Currently 'gog drive comments' supports list, get, create, update,
delete, and reply — but there's no way to resolve a comment. The Google Drive API
v3 supports resolving comments via PATCH /files/{fileId}/comments/{commentId}
with body {"resolved": true}. Add a 'resolve' subcommand to 'gog drive comments'
that marks a comment as resolved. Also add an 'unresolve' for completeness. Look
at how existing subcommands like 'reply' and 'update' are implemented and follow
the same patterns.

Follow-ups applied in the same branch:
- add `gog drive create` (Drive API gap vs `gog docs create`)
- test resolve/unresolve against the live API (discovered "unresolve" is invalid;
  correct action is "reopen")
- add `gog docs comments unresolve` for symmetry
- add unit tests for all three new commands
```

## Before filing

Create branch `feat/drive-create-resolve-unresolve` — changes are currently uncommitted on local `main`.

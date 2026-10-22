# Local patched build

Base: upstream v0.46.0. Branch: tw/update-v0.46.0.

Preserved custom commands:

- `drive create NAME --type doc|sheet|slides [--parent ID]`
- `docs style ID --style STYLE --paragraphs SELECTION` or `--find TEXT [--through TEXT]`
- `drive comments unresolve` and `docs comments unresolve`, now aliases for upstream `reopen`.

Comment resolve/reopen uses upstream's implementation and JSON envelopes. The old
fork history remains on main; this update does not rewrite it. The installed old
binary was built from a dirty tree, so its unrecorded modifications cannot be
reconstructed from Git alone. Retain it as a rollback copy during migration.

Build with `make build`; test with `go test ./...`. Tests use mock services,
not live Workspace writes. No OAuth configuration migration is performed here.

For the next release, update this small patch branch rather than merging the old
main branch, whose upstream contributions were squash-merged with different SHAs.

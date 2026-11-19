# Proposal

## Why

Discovered MCP tool descriptions are always displayed in the admin server cards, making the page harder to scan when a server exposes many tools. Administrators need to hide the details without changing the server or its saved tool allowlist, and checkbox updates should not unexpectedly change whether the list is visible.

## What Changes

- Add an independent show/hide control for each MCP server's discovered-tools list.
- Keep the server card and its configuration visible when the discovered-tools list is hidden.
- Preserve the list's current visibility when an administrator enables or disables a tool with its checkbox, including across the allowlist refresh.
- Keep hiding purely presentational: it does not change the server's discovered tools, enabled state, or saved allowlist.
- Retain the existing Discover Tools action for refreshing the server's discovered tool data.

## Capabilities

### New Capabilities

<!-- No new capabilities are introduced. -->

### Modified Capabilities

- `admin-governance-console`: Specify independent visibility controls for discovered MCP tools and preserve visibility through allowlist updates.

## Impact

- `web/src/app/admin/page.tsx`: MCP server card controls and per-server discovered-tools visibility state.
- `web/src/__tests__/`: focused coverage for hiding/showing discovered tools and preserving visibility during tool allowlist updates.
- No API, database, or MCP protocol changes are expected.

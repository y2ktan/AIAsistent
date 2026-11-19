# Design

## Context

The MCP Servers tab currently renders each server's discovered tool list whenever tools are present. Tool checkbox updates save the allowlist and reload all admin data; there is no UI state for hiding a list. See proposal.md for the motivation and specs/admin-governance-console/spec.md for the behavior contract.

## Goals / Non-Goals

**Goals:**
- Let administrators show or hide discovered tools independently for each MCP server.
- Keep visibility state stable through tool allowlist updates and data reloads.
- Keep the Discover Tools action independent from hiding or showing existing results.

**Non-Goals:**
- Change MCP discovery, tool permissions, server enablement, or persistence behavior.
- Add global controls, search, or filtering for discovered tools.

## Decisions

- Track hidden server IDs in local component state. An empty set means existing discovered lists remain visible by default; hiding one server does not affect another.
- Add a separate Show Tools / Hide Tools button for each server with discovered tools. The button only changes local visibility state; the existing Discover Tools button continues to refresh discovery data.
- Do not change visibility state when a checkbox update or discovery request reloads server data. This prevents a data refresh from overriding the administrator's presentation choice.
- Keep the visibility state in the admin page rather than the API or database because it is a view preference and must not affect persisted configuration.
- Alternatives considered: a single page-wide hide control would not allow independent server management; using Discover Tools as a disclosure toggle would mix refreshing data with presentation and make repeated discovery less clear.

## Risks / Trade-offs

- [Visibility preference resets when the admin page is remounted or reloaded] → Keep the existing expanded-by-default behavior and preserve state across in-page API refreshes; durable preference persistence is out of scope.
- [A hidden list could obscure whether a server has discovered tools] → Keep the disclosure control on the visible server card and label it Show Tools / Hide Tools.

## Migration Plan

No migration is required. The change is limited to admin UI state and controls; existing server records and allowlists remain unchanged.

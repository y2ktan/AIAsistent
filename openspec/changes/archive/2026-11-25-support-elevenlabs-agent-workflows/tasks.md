# Tasks

## 1. Admitting supported hosted workflows

- [x] 1.1 Model and validate the documented workflow graph, supported node and edge types, and workflow tool references; reject malformed graphs, unknown/client-only tools, and unresolved references before activation or token issuance. Add table-driven validator tests for supported and rejected graphs.
- [x] 1.2 Extend hosted-agent inspection to verify provider-managed server-side tools and MCP-server references while retaining private-agent, version, authentication, and client-override checks. Add mocked API tests for valid, missing, and mismatched resource IDs.
- [x] 1.3 Verify hosted prompt and Knowledge Base synchronization preserves the existing workflow graph and provider-managed tools. Add a regression test that inspects the exact PATCH payload and confirms it does not overwrite workflow-owned fields.

## 2. Persisting per-server hosted integration choices

- [x] 2.1 Add an additive persistence migration and update `MCPServerRecord`, PostgreSQL queries, store mocks, and strict admin request/response handling for `disabled`/`direct`/`proxy` mode, provider resource mapping, proxy credential, sync status, and durable creation-intent ID/configuration/attempt state. Keep credentials in intent configuration encrypted at rest and omit that configuration from API responses. Default existing and new servers to disabled with no creation intent; test defaults, upgrades, and secret omission from API responses.
- [x] 2.2 Update MCP admin CRUD to validate mode-specific inputs, preserve encrypted credentials when fields are omitted, and retain a disabled record when remote cleanup is incomplete. Add tests for create, update, disable, delete, and provider-sync failure state.

## 3. Implementing the authenticated MCP proxy

- [x] 3.1 Add and document the public HTTPS `HOSTED_MCP_PROXY_BASE_URL` setting; reject proxy configuration when it is absent or invalid. Add a server-to-server MCP endpoint limited to initialization, allowlisted tool listing, and tool calls, using the existing MCP host for outbound calls.
- [x] 3.2 Authenticate each request with the per-server credential and enforce current server status and tool allowlist before forwarding. Apply bounded JSON-RPC payloads, rate limits, timeouts, strict method handling, safe responses, and secret-safe diagnostics; add tests proving invalid credentials and disallowed tools never reach the upstream server.

## 4. Reconciling ElevenLabs MCP resources

- [x] 4.1 Implement mocked and bounded ElevenLabs MCP-server create/list/read/update/delete operations and an idempotent reconciler that manages only mapped application resources. Persist creation intent and serialize attempted creation durably across instances before POST; include a non-secret namespaced ownership marker in provider `request_meta`. Recover ambiguous creation only through exactly one verified marker and intended-configuration match, persisting the mapping before management. Never automatically re-create after an ambiguous attempt or infer ownership from display names. Require public HTTPS and exact discovered-tool allowlist eligibility for direct mode; configure proxy mode with its public application endpoint and dedicated credential.
- [x] 4.2 Integrate reconciliation with hosted resource sync and MCP changes. Preserve dashboard-owned tools/workflows, record pending/synced/failed, unresolved-creation/recovery, or incomplete-revocation status, support retry without duplicates, and keep ownership and creation-intent state until remote deletion is verified. Test mode changes, partial failures, retries, cleanup isolation, timeout-after-create, failed mapping persistence, restart, concurrent reconciliation, and absent/duplicate/mismatched marker recovery. Unverifiable recovery must remain unavailable without another POST, including during disable/delete.
- [x] 4.3 Revalidate hosted agent version and MCP synchronization state before issuing access; block access when a referenced application resource is not synchronized or revocation is incomplete. Add token-admission tests for ready and failed states.

## 5. Updating admin controls and hosted skill alignment

- [x] 5.1 Add per-server disabled/direct/proxy controls, direct credential-sharing confirmation, proxy/external-action disclosures, sanitized sync, unresolved-creation/recovery and revocation status, and retry behavior to the MCP admin UI. Explain that uncertain recovery remains blocked and may require administrator assistance, and that the non-secret provider ownership marker may reach the upstream service. Add focused UI tests for defaults, mode explanations, and failed states.
- [x] 5.2 Validate enabled Agent Skill tool references against the reconciled hosted tool inventory so prompt guidance cannot introduce unavailable tools. Add tests for available and disabled/unshared MCP tools and verify hosted mode does not start the local tool loop.

## 6. Integration verification

- [x] 6.1 Run focused Go tests for hosted validation, MCP persistence/proxy/reconciliation, and API routes; run the applicable web tests and type/lint checks; run `openspec validate --strict` and confirm all proposal capabilities have matching delta specs.

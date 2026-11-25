# web-app-security-hardening Specification

## Purpose

Establishes mandatory web, API, data, and runtime security controls aligned with A10 Networks Web Application Security Best Practices across all application services.

## Requirements

### Requirement: Strict input validation, parameterized queries, and SSRF prevention
The system SHALL validate all HTTP, signaling, and tool payloads against strict schemas, execute database operations exclusively via parameterized queries, and block private/loopback/link-local addresses on administrator-supplied outbound URLs unless explicitly allowlisted.

#### Scenario: Malformed or SQL-injection payload submitted to API
- **WHEN** a client sends unexpected fields, invalid types, or SQL metacharacters in any request parameter or body
- **THEN** the system rejects invalid schemas with HTTP `400 Bad Request` or safely parameterizes the input without executing injected commands

#### Scenario: Administrator configures internal metadata IP as AI or MCP endpoint
- **WHEN** an administrator submits a cloud metadata, loopback, or private-network URL (such as `http://169.254.169.254`) when private targets are not allowlisted
- **THEN** the system blocks outbound resolution and rejects the configuration with HTTP `400 Bad Request`

### Requirement: API rate limiting, CSRF protection, and object-level authorization
The system SHALL enforce per-IP and per-account rate limits on authentication, signaling, ingestion, and admin endpoints, require CSRF verification on cookie-authenticated state-changing requests, and enforce object-level authorization checks on every resource access.

#### Scenario: Brute-force login or signaling flood detected
- **WHEN** a client exceeds the configured request rate threshold on `/api/auth/login` or signaling endpoints
- **THEN** the system responds with HTTP `429 Too Many Requests` and a `Retry-After` header

#### Scenario: Cross-site state-changing request lacks CSRF proof
- **WHEN** a browser submits a cookie-authenticated POST, PUT, PATCH, or DELETE request with a mismatched `Origin` or missing CSRF token
- **THEN** the system rejects the request with HTTP `403 Forbidden`

### Requirement: Cryptographic protection of secrets at rest and data in transit
The system SHALL encrypt stored third-party API keys and MCP credentials at rest using authenticated encryption (AES-256-GCM), hash user passwords with Argon2id, and redact sensitive values from all logs and error responses.

#### Scenario: Database config row or application log inspected
- **WHEN** stored provider credentials or application error logs are inspected
- **THEN** secrets appear only as AES-256-GCM ciphertext in storage and never appear in plaintext in logs or HTTP error bodies

### Requirement: Hardened HTTP security headers and runtime posture
The system SHALL emit strict HTTP security headers (`Content-Security-Policy`, `Strict-Transport-Security`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy`, and `Permissions-Policy` scoped to self for `camera` and `microphone`) and run service containers as non-root users without exposed debug endpoints.

#### Scenario: Client inspects HTTP response headers on web and API routes
- **WHEN** any page or API response is returned by the system
- **THEN** the response includes the hardened security headers and omits internal server version banners or stack traces

### Requirement: Hosted MCP proxy authorization
The hosted MCP proxy SHALL authenticate each provider request with a dedicated per-server credential and SHALL authorize the server and tool against current application policy before forwarding any request.

#### Scenario: Proxy request is unauthenticated
- **WHEN** a provider request reaches the proxy without a valid credential for the requested MCP server
- **THEN** the proxy rejects it before contacting the configured MCP endpoint

#### Scenario: Proxy request targets an unavailable tool
- **WHEN** an authenticated request targets a disabled server or a tool not currently enabled and allowlisted for that server
- **THEN** the proxy denies the request before forwarding it

#### Scenario: Proxy forwards an authorized tool call
- **WHEN** a request has a valid per-server credential and targets a currently enabled, allowlisted tool on an eligible endpoint
- **THEN** the proxy forwards only that tool call using the application's stored endpoint authentication and existing outbound URL safety checks

#### Scenario: Proxy integration is disabled or revoked
- **WHEN** an administrator disables, removes, or rotates a server's hosted proxy integration
- **THEN** the previous proxy credential is rejected for subsequent requests

#### Scenario: Proxy handles secrets and failures
- **WHEN** a proxy request succeeds or fails
- **THEN** diagnostics omit proxy credentials, upstream authentication material, and raw tool arguments, while reporting a sanitized outcome

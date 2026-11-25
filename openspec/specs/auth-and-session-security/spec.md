# auth-and-session-security Specification

## Purpose

Governs user authentication, role-based access control, secure session lifecycle management, and short-lived media signaling authorization across all protected pages and endpoints.

## Requirements

### Requirement: Credential-based login gating protected routes
The system SHALL require unauthenticated users attempting to access the conversational home page or administrative portal to authenticate via the login page before granting access.

#### Scenario: Unauthenticated user visits home page
- **WHEN** an unauthenticated user navigates to `/` or `/admin`
- **THEN** the system redirects the user to `/login` without rendering protected content or issuing media tokens

#### Scenario: Valid credentials establish session
- **WHEN** an active user submits valid email and password credentials on `/login`
- **THEN** the system issues an `HttpOnly`, `Secure`, `SameSite=Strict` session cookie and redirects the user to `/`

#### Scenario: Invalid or disabled credentials rejected generically
- **WHEN** a user submits an invalid password, unknown email, or deactivated account on `/login`
- **THEN** the system returns a uniform authentication failure response without revealing whether the account exists

### Requirement: Rotating session lifecycle and immediate revocation
The system SHALL enforce bounded session lifetimes, rotate session identifiers upon privilege authentication, and immediately invalidate server-side sessions upon logout or account deactivation. Existing backend media SHALL terminate on revocation. Direct hosted media SHALL disconnect when the application client detects invalid authentication; immediate termination of an uncooperative provider-connected client is not guaranteed.

#### Scenario: User logs out
- **WHEN** an authenticated user triggers logout
- **THEN** the system revokes the server-side session record, clears the session cookie, and rejects any subsequent request presenting that token

#### Scenario: Deactivated user session revoked mid-flight
- **WHEN** an administrator deactivates an active user account
- **THEN** the system invalidates all active application sessions for that user, terminates active backend signaling/media sessions, and rejects subsequent hosted access issuance

#### Scenario: Hosted client detects revocation
- **WHEN** a hosted client detects invalid application authentication during its active-session check or foreground revalidation
- **THEN** it ends the provider connection and releases its audio resources without claiming the backend can force an uncooperative direct connection to end

### Requirement: Zero-Trust role-based access control
The system SHALL verify both session validity and assigned role (`user` or `admin`) on every HTTP API request and signaling handshake.

#### Scenario: Standard user attempts admin access
- **WHEN** an authenticated user with role `user` requests `/admin` or any administrative API endpoint
- **THEN** the system denies the request with HTTP `403 Forbidden` and records an authorization failure event

### Requirement: Single-use ephemeral signaling tokens
In `cascaded` and `realtime` modes, the system SHALL require a short-lived, single-use token bound to the authenticated user session before establishing a backend audio peer connection. Hosted mode SHALL instead require authenticated server issuance of provider-defined connection access and SHALL NOT claim the local signaling token's single-use semantics for provider tokens.

#### Scenario: Valid signaling token consumed once
- **WHEN** an authenticated client exchanges a fresh signaling token to initiate a backend media peer connection
- **THEN** the system accepts the handshake and marks the token consumed so replay attempts are rejected with HTTP `401 Unauthorized`

#### Scenario: Hosted access uses provider token
- **WHEN** an active authenticated user starts hosted voice
- **THEN** the application obtains provider connection access through its protected issuer without creating or consuming a local backend signaling token

### Requirement: Initial bootstrap without hardcoded default credentials
The system SHALL refuse to create or enable default administrative credentials and SHALL require an explicit bootstrap secret or one-time initialization flow when no administrator exists.

#### Scenario: Startup with empty user store
- **WHEN** the system starts with zero registered users
- **THEN** no default username/password login succeeds unless provisioned through the configured bootstrap secret

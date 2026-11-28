# Spec Delta

## MODIFIED Requirements

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

### Requirement: Single-use ephemeral signaling tokens
In `cascaded` and `realtime` modes, the system SHALL require a short-lived, single-use token bound to the authenticated user session before establishing a backend audio/video peer connection. Hosted mode SHALL instead require authenticated server issuance of provider-defined connection access and SHALL NOT claim the local signaling token's single-use semantics for provider tokens.

#### Scenario: Valid signaling token consumed once
- **WHEN** an authenticated client exchanges a fresh signaling token to initiate a backend media peer connection
- **THEN** the system accepts the handshake and marks the token consumed so replay attempts are rejected with HTTP `401 Unauthorized`

#### Scenario: Hosted access uses provider token
- **WHEN** an active authenticated user starts hosted voice
- **THEN** the application obtains provider connection access through its protected issuer without creating or consuming a local backend signaling token

# Spec Delta

## ADDED Requirements

### Requirement: Hosted language-policy boundary
Application-managed language selection, default-language resolution, per-language voice assignment, and unsupported-language notices SHALL apply to existing backend modes only. Hosted sessions SHALL use dashboard-managed language and voice settings without applying the application's runtime language policy or promising identical language coverage.

#### Scenario: Hosted voice differs from saved local voice
- **WHEN** the dashboard agent has a different voice or language configuration from the application's saved language table
- **THEN** the hosted session uses the dashboard configuration without sending local voices as overrides

#### Scenario: Unsupported language in hosted mode
- **WHEN** a user speaks a language to the hosted agent
- **THEN** the agent's configured language behavior applies without the application injecting its default-language or once-per-session fallback notice

### Requirement: Local language configuration survives hosted selection
Selecting hosted mode SHALL NOT alter the application's saved supported languages, default language, or either provider's voice assignments. Hosted activation SHALL NOT require a local default-language voice. Editing retained local settings SHALL preserve their existing invariants for later backend sessions.

#### Scenario: Hosted activation without local ElevenLabs voice
- **WHEN** an administrator activates a valid hosted agent while the local language table lacks a default ElevenLabs voice
- **THEN** hosted activation succeeds without inventing a local voice mapping or changing the table

#### Scenario: Return to backend language settings
- **WHEN** an administrator returns to cascaded mode
- **THEN** the selected speech provider's existing default-voice validation applies and new sessions use the retained application language settings

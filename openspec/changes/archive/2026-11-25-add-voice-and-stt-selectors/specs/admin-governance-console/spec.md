# Spec Delta

## MODIFIED Requirements

### Requirement: Write-only AI provider key, endpoint, and model configuration
The system SHALL allow administrators to configure active AI pipeline mode (`realtime` or `cascaded`), provider base URLs, model names (including selecting from discovered speech-to-text models when available, with manual entry fallback), system instructions, and API keys while treating secrets as write-only after submission.

#### Scenario: Administrator updates AI API key and endpoint
- **WHEN** an administrator saves a new AI provider endpoint and API key
- **THEN** the system encrypts the key at rest, applies the configuration to subsequent sessions, and returns only a masked key preview (for example, `****abcd`) on all read responses

#### Scenario: Administrator selects an STT model from discovered provider models
- **WHEN** the configured speech-to-text provider exposes available models and an administrator opens `/admin` in `cascaded` mode
- **THEN** the AI Agent form offers an STT Model dropdown populated with discovered models, falling back to a text input when discovery is unavailable

#### Scenario: Administrator tests AI endpoint connectivity
- **WHEN** an administrator triggers a connection health check for the configured AI provider
- **THEN** the backend verifies connectivity server-side and returns a sanitized pass/fail status without exposing stored credentials to the browser

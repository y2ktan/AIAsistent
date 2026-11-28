# Spec Delta

## MODIFIED Requirements

### Requirement: Progressive skill discovery in agent context
In `cascaded` and `realtime` modes, the system SHALL expose enabled Agent Skills to the active conversational agent via a compact catalog of skill names and descriptions in the system prompt rather than injecting every full skill body into every turn. In `elevenlabs_agent` mode, the system SHALL instead synchronize enabled Agent Skills' Markdown instructions and bundled UTF-8 reference files server-side into the hosted agent's prompt beneath the immutable security preamble within bounded size limits, omitting disabled skills.

#### Scenario: Enabled skills included in session catalog while disabled skills are omitted
- **WHEN** a user starts or continues a `cascaded` or `realtime` session while two skills are enabled and one skill is disabled
- **THEN** the agent's system instructions include only the two enabled skills' names and descriptions in the available-skills catalog

#### Scenario: Hosted agent prompt synchronizes enabled skills and omits disabled skills
- **WHEN** `elevenlabs_agent` mode is active (or activated) while two skills are enabled and one skill is disabled
- **THEN** the server updates the hosted agent's prompt with only the two enabled skills' instructions and bundled UTF-8 references beneath the security boundary preamble, and subsequent skill uploads, toggles, or deletions resynchronize the hosted prompt

## ADDED Requirements

### Requirement: Hosted runtime-tool exclusion
The initial hosted mode SHALL NOT expose or execute runtime backend skill loaders (`load_skill`, `read_skill_file`) or application MCP tools. While enabled declarative Agent Skills synchronize server-side into the hosted prompt, MCP server administration SHALL remain scoped to existing backend modes without synchronizing MCP servers or tool credentials to ElevenLabs.

#### Scenario: Hosted session cannot invoke the local backend MCP loop
- **WHEN** a hosted conversation requests a live external MCP lookup
- **THEN** the application does not start a local tool loop, and only a separately validated provider-managed or explicitly selected hosted integration can execute the operation

#### Scenario: Oversized skill bundle rejected for hosted prompt sync
- **WHEN** enabled Agent Skills exceed the configured hosted prompt size budget during hosted activation or skill synchronization
- **THEN** synchronization fails safely with a clear diagnostic without truncating the security preamble or exposing secrets

#### Scenario: Existing backend skill behavior preserved
- **WHEN** a user returns to a cascaded or realtime session with enabled skills and allowlisted MCP tools
- **THEN** progressive loading and governed tool execution remain available according to their existing configuration

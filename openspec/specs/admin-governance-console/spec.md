# admin-governance-console Specification

## Purpose

Provides administrators with secure management workflows for user accounts and roles, RAG document ingestion, write-only AI agent credentials and endpoints, Agent Skills, MCP server connections, and security audit logs.

## Requirements

### Requirement: User account and role lifecycle management
The system SHALL allow administrators on `/admin` to create users, assign or update roles (`user` or `admin`), activate or deactivate accounts, and prevent removal or demotion of the last active administrator.

#### Scenario: Administrator creates a new user account
- **WHEN** an administrator submits a valid email, password policy-compliant credential, and role on `/admin`
- **THEN** the system creates the account with hashed credentials, logs an audit entry, and lists the user in the directory

#### Scenario: Administrator attempts to deactivate the sole remaining admin
- **WHEN** an administrator attempts to deactivate or demote the only remaining active `admin` account
- **THEN** the system rejects the operation with HTTP `409 Conflict` to prevent administrative lockout

### Requirement: RAG document management workspace
The system SHALL provide an administrative document panel displaying local ingestion status, chunk counts, upload controls, reindex triggers, and deletion actions, and when an ElevenLabs hosted agent is configured SHALL additionally display per-document hosted Knowledge Base sync status (`synced`, `indexing`, `failed`, or `not_synced`) and a manual hosted-sync/retry trigger.

#### Scenario: Administrator monitors document ingestion progress
- **WHEN** an administrator uploads one or more documents in `/admin`
- **THEN** the document table reflects status transitions (`processing`, `ready`, or `failed`) and total indexed chunk count

#### Scenario: Administrator monitors and retries hosted Knowledge Base sync
- **WHEN** an administrator views `/admin` with a configured hosted agent or triggers a hosted sync retry after an upstream ElevenLabs failure
- **THEN** the document panel displays each document's hosted sync state without altering its local pgvector status and retries synchronization server-side

### Requirement: Write-only AI provider key, endpoint, and model configuration
The system SHALL let administrators configure AI pipeline mode, provider endpoints and models, system instructions, and write-only credentials, with discovered speech-to-text models where available and manual fallback. In hosted mode, the application SHALL synchronize only `assistantName`, `systemInstructions`, enabled Agent Skills, and `ready` local documents; provider model, voice, TTS, and language settings SHALL remain dashboard-owned.

#### Scenario: Administrator updates AI API key and endpoint
- **WHEN** an administrator saves a new AI provider endpoint and API key
- **THEN** the system encrypts the key at rest, applies the configuration to subsequent sessions, and returns only a masked key preview (for example, `****abcd`) on all read responses

#### Scenario: Administrator selects an STT model from discovered provider models
- **WHEN** the configured speech-to-text provider exposes available models and an administrator opens `/admin` in `cascaded` mode
- **THEN** the AI Agent form offers an STT Model dropdown populated with discovered models, falling back to a text input when discovery is unavailable

#### Scenario: Administrator tests AI endpoint connectivity
- **WHEN** an administrator triggers a connection health check for the configured AI provider
- **THEN** the backend verifies connectivity server-side and returns a sanitized pass/fail status without exposing stored credentials to the browser

#### Scenario: Administrator selects hosted mode
- **WHEN** an administrator selects `elevenlabs_agent`
- **THEN** the form offers a private agent ID, the existing write-only ElevenLabs credential, `assistantName`, and `systemInstructions`, identifies which settings synchronize automatically versus remain dashboard-owned, and preserves inactive provider fields

### Requirement: Hosted-agent activation validation and synchronization
The system SHALL require an agent ID and ElevenLabs credential for hosted activation, validate private authentication plus every workflow and tool reference, and synchronize the application-owned persona, skills, and `ready` documents without requiring inactive local provider settings.

#### Scenario: Complete private agent activated and synchronized
- **WHEN** an administrator activates a reachable private agent using a valid write-capable ElevenLabs credential and every configured workflow, tool, and MCP reference passes validation
- **THEN** the server synchronizes the application's prompt/persona, enabled Agent Skills, and `ready` local documents to the hosted agent, enables hosted RAG while preserving unmanaged dashboard documents, and saves `elevenlabs_agent` without changing local voices, language defaults, or provider settings

#### Scenario: Incomplete, incompatible, or non-writable agent
- **WHEN** an administrator attempts activation with missing credentials, a missing or inaccessible agent, insufficient write permissions, an oversized skill prompt, a public agent, an unsupported workflow, or an unverified external tool
- **THEN** the save fails with a safe diagnostic and leaves the previous active mode unchanged

#### Scenario: Provider validation unavailable
- **WHEN** the provider cannot be reached during activation validation or synchronization
- **THEN** activation fails visibly without treating unavailable validation as success

### Requirement: Hosted ownership and processing disclosure
Hosted settings SHALL distinguish synchronized application settings from ElevenLabs dashboard-owned settings and disclose cloud processing, potential charges, provider turn-taking, explicitly selected MCP exposure, and absent camera or local citation capabilities.

#### Scenario: Administrator configures hosted mode
- **WHEN** an administrator opens the hosted-agent settings
- **THEN** the form displays which settings synchronize automatically versus remain dashboard-owned and hides inapplicable local speech/LLM endpoint controls while preserving their saved values

#### Scenario: Local document operations synchronize when hosted mode is active
- **WHEN** an administrator uploads, reindexes, or deletes a document in the application's document workspace while `elevenlabs_agent` is active
- **THEN** the operation updates local pgvector storage and automatically synchronizes the corresponding ElevenLabs Knowledge Base entry while preserving unmanaged dashboard documents

### Requirement: Audio focus noise-filtering configuration
The system SHALL enable Audio Focus by default and SHALL allow administrators on `/admin` to enable or disable Audio Focus speaker voice isolation and environmental noise filtering, recording configuration changes in the administrative audit log.

#### Scenario: Audio Focus is enabled by default before manual customization
- **WHEN** an installation initializes or an administrator opens the AI Agent settings on `/admin` before changing Audio Focus
- **THEN** Audio Focus is reported and rendered as enabled (`true`) by default

#### Scenario: Administrator disables or re-enables Audio Focus on /admin
- **WHEN** an administrator toggles Audio Focus off or back on and saves AI Agent settings on `/admin`
- **THEN** the system persists the updated Audio Focus state, applies it to subsequent voice sessions, and records an administrative audit event

#### Scenario: Authenticated caller retrieves active Audio Focus session setting safely
- **WHEN** an authenticated non-admin or admin user starts a voice session
- **THEN** the client receives the active `audioFocusEnabled` boolean without exposing provider URLs, credentials, or admin-only settings

### Requirement: Assistant wake-name and alias configuration
The system SHALL default the assistant name to `Aegis` with no wake aliases and SHALL allow administrators on `/admin` to configure a non-empty primary assistant name and optional wake aliases in English or Chinese, recording updates in the administrative audit log and exposing the active primary name safely to authenticated voice sessions.

#### Scenario: Default assistant name reported before customization
- **WHEN** an installation initializes or an administrator opens the AI Agent settings on `/admin` before customizing the assistant name
- **THEN** the primary assistant name is reported and rendered as `Aegis` with an empty wake alias list

#### Scenario: Administrator saves English and Chinese wake names on /admin
- **WHEN** an administrator sets a primary assistant name (such as `Jarvis` or `小智`) and optional wake aliases (such as `["小智", "Aegis"]`) and saves AI Agent settings on `/admin`
- **THEN** the system persists the trimmed primary name and deduplicated wake aliases, applies them to subsequent voice sessions, and records an administrative audit entry

#### Scenario: Blank primary assistant name rejected
- **WHEN** an administrator submits an empty or whitespace-only primary assistant name on `/admin`
- **THEN** the system rejects the request with HTTP `400 Bad Request` and leaves the stored configuration unchanged

#### Scenario: Authenticated caller retrieves configured assistant name safely
- **WHEN** an authenticated non-admin or admin user fetches `/api/v1/rtc/config` before starting a voice session
- **THEN** the response includes the active `assistantName` alongside `audioFocusEnabled` without exposing provider URLs, credentials, or admin-only settings

### Requirement: Paired cascaded speech provider selection
The system SHALL allow administrators to select the existing OpenAI-compatible provider or ElevenLabs for both speech-to-text and text-to-speech in cascaded mode. The selection SHALL apply to new sessions and SHALL NOT change the configured language model.

#### Scenario: Existing installation remains local
- **WHEN** an installation is upgraded without changing its speech provider
- **THEN** new cascaded sessions continue using its existing speech endpoints, models, credentials, and voices

#### Scenario: Administrator selects ElevenLabs
- **WHEN** an administrator saves ElevenLabs as the speech provider with a configured key and default-language voice
- **THEN** new cascaded sessions use ElevenLabs for both speech services and continue using the existing language model

#### Scenario: Administrator returns to the existing provider
- **WHEN** an administrator switches back to the existing provider
- **THEN** new cascaded sessions use the previously saved existing speech settings

#### Scenario: Selection is not applied to realtime
- **WHEN** a session uses realtime mode
- **THEN** the cascaded speech provider selection does not change its realtime provider

#### Scenario: Non-administrator attempts to change provider
- **WHEN** a non-administrator attempts to change the speech provider
- **THEN** the system rejects the request without changing the saved configuration

### Requirement: Separate write-only ElevenLabs credential
The system SHALL allow administrators to configure the ElevenLabs API key, store it encrypted separately from the existing provider key, and expose only a masked preview showing its last five characters after submission. Updating or omitting either key SHALL NOT replace or disclose the other key.

#### Scenario: Save and read an ElevenLabs key
- **WHEN** an administrator saves an ElevenLabs key and later reads the configuration
- **THEN** the key is encrypted at rest and only a masked preview showing its last five characters is returned

#### Scenario: Key is too short to mask safely
- **WHEN** an administrator submits a non-empty ElevenLabs key of five characters or fewer
- **THEN** the system rejects the update without revealing the key or changing the stored configuration

#### Scenario: Save unrelated configuration
- **WHEN** an administrator updates other settings without providing an ElevenLabs key
- **THEN** the existing ElevenLabs key remains unchanged

#### Scenario: Credential and configuration audit
- **WHEN** an administrator changes the speech provider or its credential
- **THEN** an audit event records the change without the key or other secret material

### Requirement: ElevenLabs configuration and validation
The system SHALL provide separate ElevenLabs speech model settings with balanced defaults and SHALL make the provider's cloud data transmission and potential charges clear before selection.

#### Scenario: ElevenLabs configuration is incomplete
- **WHEN** an administrator attempts to activate ElevenLabs without a saved or submitted key or a voice for the default language
- **THEN** the save is rejected with a clear message and the previously active provider remains unchanged

#### Scenario: Provider configuration and discovery
- **WHEN** an administrator configures ElevenLabs
- **THEN** the interface offers its model settings and its available voices when discovery succeeds, with manual voice ID entry when discovery is unavailable

### Requirement: MCP server registration and policy configuration
The system SHALL allow administrators to register, enable, disable, and test Model Context Protocol (MCP) server connections with explicit transport settings, visible tool descriptions, tool allowlists, and independent visibility controls for each server's discovered tool list.

#### Scenario: Administrator registers and tests an MCP server
- **WHEN** an administrator configures an MCP server URL/transport and runs tool discovery
- **THEN** the system validates the target against outbound security policies, lists discovered tools with their descriptions, and allows toggling both server enablement and which tools are exposed to the agent

#### Scenario: Administrator disables an MCP server without deleting it
- **WHEN** an administrator toggles an existing MCP server to disabled on `/admin`
- **THEN** the server configuration and allowlist remain saved, and none of its tools are exposed to or callable by the conversational agent until re-enabled

#### Scenario: Administrator hides one server's discovered tools
- **WHEN** an administrator hides the discovered-tools list for an MCP server
- **THEN** only that server's discovered-tools list is collapsed, while the server card, server state, discovered tools, and saved allowlist remain unchanged

#### Scenario: Tool checkbox update preserves discovered-tools visibility
- **WHEN** an administrator enables or disables a discovered tool with its checkbox
- **THEN** the system saves the updated allowlist and preserves that server's current discovered-tools visibility

### Requirement: Hosted MCP integration governance
The administration console SHALL let an administrator explicitly select and review how each application MCP server is exposed to a hosted ElevenLabs agent.

#### Scenario: Hosted integration defaults to disabled
- **WHEN** an administrator creates or opens an application MCP server that has no hosted integration choice
- **THEN** the console shows that it is not shared with hosted agents and does not register it with ElevenLabs

#### Scenario: Administrator chooses direct registration
- **WHEN** an administrator selects direct provider registration for an eligible MCP server
- **THEN** the console explains that the endpoint and configured authentication material are shared with ElevenLabs and requires confirmation before saving that choice

#### Scenario: Administrator chooses the application proxy
- **WHEN** an administrator selects the application proxy for an MCP server
- **THEN** the console explains that ElevenLabs will invoke the application over HTTPS, that the original MCP credential remains in the application, and which allowlisted tools can be called

#### Scenario: Administrator reviews synchronization state
- **WHEN** the hosted MCP integration is pending, synchronized, or failed
- **THEN** the console shows that state and a sanitized reason, and does not represent a failed integration as ready for hosted use

#### Scenario: Administrator disables or removes an integration
- **WHEN** an administrator disables or removes an MCP server's hosted integration
- **THEN** the console confirms the resulting hosted availability and the system prevents new hosted calls from using that integration

### Requirement: Hosted workflow external-action disclosure
Before enabling hosted workflows with external tools, the administration console SHALL disclose that provider-executed workflow actions can send conversation-derived data to external services and may cause external side effects.

#### Scenario: Administrator enables a workflow with external actions
- **WHEN** an administrator saves a hosted configuration containing a supported external workflow action or selected MCP integration
- **THEN** the console presents the external-processing and side-effect disclosure before the configuration becomes active

### Requirement: Agent skill governance workspace
The system SHALL provide a dedicated Agent Skills tab on `/admin` allowing administrators to upload `.md` or `.zip` skill packages, replace an existing skill by name, inspect parsed metadata and instructions, view referenced MCP tool readiness, toggle skill enablement, and delete skills.

#### Scenario: Administrator uploads and inspects an Agent Skill on /admin
- **WHEN** an administrator uploads a valid `SKILL.md` or `.zip` skill bundle in the Agent Skills tab on `/admin`
- **THEN** the skill list displays the parsed skill name, description, source format, reference file count, referenced MCP tool availability, enablement toggle, instructions preview, and delete action

#### Scenario: Administrator re-uploads an existing skill to update it
- **WHEN** an administrator uploads a valid skill package whose normalized `name` matches an existing skill
- **THEN** the system updates the existing skill's instructions, description, and bundled references in place while preserving its identifier and recording an audit event

#### Scenario: Administrator disables or deletes an Agent Skill
- **WHEN** an administrator toggles an Agent Skill to disabled or deletes it on `/admin`
- **THEN** subsequent agent turns omit the skill from active discovery and activation, and the action is recorded in the audit log

### Requirement: Immutable administrative security audit log
The system SHALL record structured audit events for logins, failed authentications, user/role mutations, document ingestion/deletions, Agent Skill uploads/updates/deletions, and AI/MCP configuration updates, viewable by administrators.

#### Scenario: Configuration change recorded in audit trail
- **WHEN** an administrator modifies AI credentials, MCP settings, Agent Skills, or user roles
- **THEN** an append-only audit record capturing actor ID, timestamp, action type, and redacted metadata becomes visible in the audit log view

### Requirement: Independent hosted synchronization controls
The administration console SHALL provide separate controls for synchronizing local RAG documents and enabled Agent Skills to a hosted ElevenLabs agent, SHALL default both controls to enabled for existing and new configurations, and SHALL show the selected ownership mode and synchronization state independently.

#### Scenario: Administrator disables only RAG synchronization
- **WHEN** an administrator disables application RAG synchronization while leaving Agent Skill synchronization enabled
- **THEN** hosted activation no longer uploads or attaches application documents, enabled Agent Skills continue to synchronize, and the console clearly identifies RAG as application-sync disabled

#### Scenario: Administrator disables only Agent Skill synchronization
- **WHEN** an administrator disables Agent Skill synchronization while leaving application RAG synchronization enabled
- **THEN** hosted prompt synchronization excludes enabled application skills, local documents continue to synchronize, and the console clearly identifies Agent Skills as application-sync disabled

#### Scenario: Administrator disables both synchronization domains
- **WHEN** an administrator disables application RAG and Agent Skill synchronization
- **THEN** the hosted agent can be activated and used with only the independently configured persona and system instructions, without application documents or skills blocking connection

#### Scenario: An existing configuration is upgraded
- **WHEN** an installation with an existing hosted-agent configuration receives this capability
- **THEN** both synchronization controls are treated as enabled until an administrator explicitly changes them

#### Scenario: Administrator changes a disabled domain
- **WHEN** an administrator enables or disables either synchronization control on an active hosted configuration
- **THEN** the system saves the choice transactionally with hosted compatibility verification and reports any required provider cleanup or synchronization failure without representing the domain as successfully changed

### Requirement: Admin creation and MCP registration forms suppress browser credential autofill
The system SHALL configure the `/admin` user creation form and MCP server registration form to suppress browser credential autofill so that saved administrator login emails and passwords are not pre-populated into those inputs by default.

#### Scenario: Add User form renders empty without credential autofill
- **WHEN** an administrator with saved browser credentials opens the Users tab on `/admin`
- **THEN** the Add User email and password inputs initialize empty and declare autocomplete suppression attributes so the browser does not pre-fill the administrator's saved email or password

#### Scenario: Register MCP form renders empty without credential autofill
- **WHEN** an administrator with saved browser credentials opens the MCP tab on `/admin`
- **THEN** the Register MCP server name, URL, and bearer token inputs initialize empty and declare autocomplete suppression attributes so the browser does not pre-fill the administrator's saved email or password

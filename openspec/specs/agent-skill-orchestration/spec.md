# agent-skill-orchestration Specification

## Purpose

Governs ingestion, validation, progressive discovery, on-demand loading, and skill-guided MCP tool analysis for declarative administrator-uploaded Agent Skills across conversational modes.

## Requirements

### Requirement: Declarative skill package validation and ingestion
The system SHALL accept administrator-uploaded Agent Skills as UTF-8 Markdown (`SKILL.md` or `.md`) files or `.zip` skill bundles containing a `SKILL.md` with required `name` and `description` frontmatter plus optional UTF-8 text reference files, and SHALL reject executable, binary, oversized, or path-traversal archive entries without executing any uploaded content on the host.

#### Scenario: Valid standalone SKILL.md uploaded
- **WHEN** an administrator uploads a UTF-8 `.md` file containing valid YAML frontmatter with `name` and `description` and a Markdown instruction body
- **THEN** the system persists the parsed skill metadata and instructions in an enabled state and never executes any content on the server

#### Scenario: Valid zip skill bundle with reference files uploaded
- **WHEN** an administrator uploads a `.zip` archive containing `SKILL.md` and UTF-8 reference files (`.md`, `.txt`, `.json`, or `.yaml`) within size limits
- **THEN** the system validates magic bytes and relative paths, stores `SKILL.md` alongside its bundled reference files, and indexes the skill by its normalized `name`

#### Scenario: Malformed, binary, or path-traversal skill upload rejected
- **WHEN** an uploaded skill package lacks `name` or `description` frontmatter, contains null/binary or executable files, exceeds size limits, or contains `..` / absolute archive paths
- **THEN** the system rejects the upload with HTTP `400 Bad Request` or `413 Content Too Large` without persisting partial skill data

### Requirement: Progressive skill discovery in agent context
In local modes, the system SHALL expose enabled skills through a compact catalog instead of injecting every full body. In hosted mode, it SHALL synchronize enabled skills' instructions and UTF-8 references beneath the immutable security preamble within the prompt budget, omitting disabled skills, only when hosted Agent Skill synchronization is enabled.

#### Scenario: Enabled skills included in session catalog while disabled skills are omitted
- **WHEN** a user starts or continues a `cascaded` or `realtime` session while two skills are enabled and one skill is disabled
- **THEN** the agent's system instructions include only the two enabled skills' names and descriptions in the available-skills catalog

#### Scenario: Hosted agent prompt synchronizes enabled skills and omits disabled skills
- **WHEN** `elevenlabs_agent` mode is active (or activated), Agent Skill synchronization is enabled, and two skills are enabled while one skill is disabled
- **THEN** the server updates the hosted agent's prompt with only the two enabled skills' instructions and bundled UTF-8 references beneath the security boundary preamble, and subsequent skill uploads, toggles, or deletions resynchronize the hosted prompt

#### Scenario: Hosted Agent Skill synchronization is disabled
- **WHEN** an administrator disables hosted Agent Skill synchronization
- **THEN** the hosted prompt is synchronized without application Agent Skill sections, local-mode skills remain unchanged, and subsequent skill uploads, toggles, or deletions do not update the hosted prompt

#### Scenario: Hosted agent connects without application skills
- **WHEN** hosted Agent Skill synchronization is disabled while enabled local skills are present or exceed the hosted prompt budget
- **THEN** hosted activation and access proceed using only the application persona and system instructions, without validating, compiling, or failing on those skills for hosted use

#### Scenario: Administrator re-enables hosted Agent Skill synchronization
- **WHEN** an administrator re-enables hosted Agent Skill synchronization
- **THEN** the server validates the currently enabled skills and available hosted tools, compiles them into the hosted prompt, and verifies the resulting hosted agent before saving the choice

### Requirement: Hosted runtime-tool exclusion
The hosted mode SHALL NOT expose or execute local runtime skill loaders (`load_skill`, `read_skill_file`) or start the application's local MCP tool loop. Provider-managed tools and explicitly selected application MCP integrations MAY run through ElevenLabs or the authenticated hosted proxy after server-side validation.

#### Scenario: Hosted session cannot invoke the local backend MCP loop
- **WHEN** a hosted conversation requests a live external MCP lookup
- **THEN** the application does not start a local tool loop, and only a separately validated provider-managed or explicitly selected hosted integration can execute the operation

#### Scenario: Oversized skill bundle rejected for hosted prompt sync
- **WHEN** enabled Agent Skills exceed the configured hosted prompt size budget during hosted activation or skill synchronization
- **THEN** synchronization fails safely with a clear diagnostic without truncating the security preamble or exposing secrets

#### Scenario: Existing backend skill behavior preserved
- **WHEN** a user returns to a cascaded or realtime session with enabled skills and allowlisted MCP tools
- **THEN** progressive loading and governed tool execution remain available according to their existing configuration

### Requirement: On-demand skill playbook and reference activation
The system SHALL provide built-in agent tools in both `cascaded` and `realtime` modes to load an enabled skill's full Markdown instructions and bundled reference files when a user request matches that skill.

#### Scenario: Agent loads matching skill instructions and bundled reference on demand
- **WHEN** a user asks a domain request matching an enabled skill in `cascaded` or `realtime` mode
- **THEN** the LLM invokes `load_skill` (and `read_skill_file` if needed) to retrieve the skill's full Markdown instructions and reference content before formulating tool calls or its answer

#### Scenario: Request to load unknown or disabled skill rejected safely
- **WHEN** the LLM attempts to load a skill name that does not exist or has been disabled by an administrator
- **THEN** the built-in skill loader returns a safe not-available message without exposing disabled skill content

### Requirement: Skill-guided MCP request analysis and response synthesis
When an enabled skill is activated for a user request, the system SHALL allow the LLM to follow the skill's procedural playbook to invoke permitted MCP tools with validated arguments, analyze the sanitized tool outputs, and synthesize the final spoken and transcript response.

#### Scenario: Assistant uses uploaded skill and MCP tool to answer user request
- **WHEN** a user asks a question covered by an enabled skill whose workflow calls an allowlisted MCP tool
- **THEN** the assistant loads the skill, invokes the allowlisted MCP tool with arguments matching its schema, analyzes the returned tool data according to the skill's rules, and delivers the synthesized response

### Requirement: Hosted skills respect hosted tool availability
When Agent Skills are synchronized to a hosted ElevenLabs agent, their declarative instructions SHALL be limited to tools validated and available to that hosted workflow.

#### Scenario: Skill refers to an available hosted tool
- **WHEN** an enabled skill guides the agent to use a provider-managed tool or an application MCP tool that is synchronized, enabled, and allowlisted
- **THEN** the hosted workflow may use that tool subject to the provider and application authorization controls

#### Scenario: Skill refers to an unavailable tool
- **WHEN** an enabled skill refers to a tool that is disabled, unshared, unsynchronized, or outside the application allowlist
- **THEN** the tool is not made available by the skill text and the hosted agent cannot invoke it

#### Scenario: Hosted mode has no local runtime tool loop
- **WHEN** a hosted conversation loads an enabled Agent Skill
- **THEN** the skill remains declarative hosted-agent guidance and does not trigger local skill loading or local MCP execution by the application

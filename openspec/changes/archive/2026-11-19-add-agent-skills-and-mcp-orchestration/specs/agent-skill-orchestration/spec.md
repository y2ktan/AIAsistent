# Spec Delta

## Purpose

Governs ingestion, validation, progressive discovery, on-demand loading, and skill-guided MCP tool analysis for declarative administrator-uploaded Agent Skills across conversational modes.

## ADDED Requirements

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
The system SHALL expose enabled Agent Skills to the active conversational agent via a compact catalog of skill names and descriptions in the system prompt rather than injecting every full skill body into every turn.

#### Scenario: Enabled skills included in session catalog while disabled skills are omitted
- **WHEN** a user starts or continues a `cascaded` or `realtime` session while two skills are enabled and one skill is disabled
- **THEN** the agent's system instructions include only the two enabled skills' names and descriptions in the available-skills catalog

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

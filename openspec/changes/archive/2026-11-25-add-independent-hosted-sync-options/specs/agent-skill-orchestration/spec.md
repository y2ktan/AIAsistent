# Spec Delta

## MODIFIED Requirements

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

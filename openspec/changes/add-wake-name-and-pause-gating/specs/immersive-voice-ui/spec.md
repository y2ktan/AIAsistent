# Spec Delta

## ADDED Requirements

### Requirement: Configured assistant identity display and clean gated captions
The system SHALL display the active configured assistant name on the home page header and assistant transcript speaker labels, and SHALL omit unaddressed background speech discarded by the wake-name gate from live conversation captions.

#### Scenario: Home page renders custom configured assistant name
- **WHEN** an administrator has configured a custom primary assistant name (such as `Jarvis` or `小智`) and a user opens the home page and conducts a voice session
- **THEN** the home page header and assistant transcript badge render the configured assistant name instead of a hardcoded default

#### Scenario: Unaddressed background chatter omitted from live captions
- **WHEN** background speakers talk during an active voice session without addressing a configured wake name or alias while no wake window is armed
- **THEN** the home page does not append the discarded background utterance to the live transcript list

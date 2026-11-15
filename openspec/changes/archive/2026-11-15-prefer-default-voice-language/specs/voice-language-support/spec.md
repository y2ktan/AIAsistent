# Spec Delta

## MODIFIED Requirements

### Requirement: Replies follow the user's spoken language
The system SHALL reply, in both transcript text and speech, in the language of the user's most recent utterance when that language is supported and has an assigned voice, and SHALL prefer the configured default language when disambiguating ambiguous same-script utterances or falling back when no language is detected on an initial turn.

#### Scenario: User speaks a supported language
- **WHEN** Mandarin Chinese is supported with an assigned voice and the user asks a question in Mandarin
- **THEN** the assistant's transcript reply is in Mandarin and the reply is spoken in Mandarin

#### Scenario: User switches language during a conversation
- **WHEN** a user who has been speaking English asks their next question in another supported language that has an assigned voice
- **THEN** the assistant replies to that question in the new language

#### Scenario: Utterance mixes two supported languages
- **WHEN** a single utterance mixes two supported languages
- **THEN** the assistant replies in the language that makes up most of the utterance

#### Scenario: Ambiguous same-script utterance prefers configured default language
- **WHEN** the dominant script of an utterance matches multiple configured languages and speech-to-text does not provide a matching language label, while the configured default language belongs to that script family
- **THEN** the assistant resolves the utterance to the configured default language

#### Scenario: Language of an utterance cannot be determined
- **WHEN** the language of an utterance cannot be determined, such as a one-word acknowledgement or unclear audio
- **THEN** the assistant replies in the language of its previous reply in that session, or in the configured default language (such as Mandarin Chinese when set as default) if there is no previous reply

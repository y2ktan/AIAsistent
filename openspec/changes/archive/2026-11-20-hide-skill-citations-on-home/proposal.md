# Proposal

## Why

When a `SKILL.md` file (or Agent Skill markdown definition) has been uploaded into the RAG document index, semantic retrieval returns its chunks alongside ordinary knowledge documents and renders internal `SKILL.md (%)` citation pills above the media dock on the home page during live conversations. Agent Skills are internal procedural playbooks for the assistant—not user-facing reference documents—so surfacing them as home-page citation badges exposes internal prompt instructions and clutters the conversational stage.

## What Changes

- Filter out Agent Skill / `SKILL.md` entries from the grounded citation cards rendered on the home page (`/`) during an active or ongoing conversation.
- Show standard RAG document citation badges (such as user guides, PDFs, DOCX, and non-skill Markdown/text documents) and their expandable excerpt previews on the home page only to authenticated administrators.
- Hide standard RAG citation badges and excerpt previews from non-admin users while preserving their transcript, assistant response, and conversation controls.
- Keep the top-header active tool indicator and backend RAG/Agent Skill execution pipelines unchanged (UI-only filtering as requested).

## Capabilities

### New Capabilities

<!-- No new capabilities are introduced. -->

### Modified Capabilities

- `immersive-voice-ui`: Update the real-time transcript and grounded citation drawer requirement so Agent Skill (`SKILL.md`) entries are excluded for every role, while non-skill RAG citation badges and excerpts are visible only to administrators.

## Impact

- `web/src/hooks/useVoiceAgentRTC.ts` and/or `web/src/app/page.tsx`: Client-side Agent Skill filtering plus role-aware home-page citation rendering, including safe expanded-card state handling.
- `web/src/__tests__/rtc-hook.test.ts` (or home-page unit tests): Unit tests verifying Agent Skill citations remain hidden for all roles, regular citations and excerpts display for administrators, and the citation section is omitted for non-admin users without affecting tool indicators.
- No backend API, database, WebRTC protocol, or MCP/Agent Skill orchestration changes.

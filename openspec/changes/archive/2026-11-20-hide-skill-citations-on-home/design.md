# Design

## Context

In `web/src/app/admin/page.tsx`, uploading a file to the RAG Knowledge Base sets `title` to `file.name` (for example, `SKILL.md`, or `SKILL` if uploaded without an explicit title via the API). During a voice conversation, WebRTC `citation` events deliver `CitationItem[]` (`documentId`, `documentTitle`, `chunkIndex`, `content`, `similarity`) to `useVoiceAgentRTC`, and `web/src/app/page.tsx` renders every item in `citations` as an emerald citation pill above the media dock. See `proposal.md` for why Agent Skill entries must not appear on the home page and `specs/immersive-voice-ui/spec.md` for the behavior contract.

## Goals / Non-Goals

**Goals:**
- Reliably detect and exclude Agent Skill (`SKILL.md` / skill playbook) items from the home-page citation badges during conversations.
- Show non-skill RAG document citation pills and expandable excerpts only when the authenticated home-page user is an administrator.
- Omit the citation section entirely for non-admin users without affecting transcripts, agent responses, tool indicators, or media controls.
- Keep filtering testable via a pure helper in the frontend codebase.

**Non-Goals:**
- Modifying backend RAG indexing, retrieval SQL, or Agent Skill orchestration.
- Hiding or altering the top-header `activeTool` badge.

## Decisions

- **Pure client-side citation filter helper (`filterVisibleCitations` / `isAgentSkillCitation`)**:
  - Match citations that represent an Agent Skill definition:
    1. `documentTitle` (trimmed, case-insensitive, normalized path basename) equals `skill.md`, `skill.markdown`, or `skill` when paired with skill frontmatter, OR ends with `/skill.md` / `\skill.md` (with `skill.md` / `skill.markdown` always treated as an Agent Skill file).
    2. Chunk `content` starts with Agent Skill YAML frontmatter (`---` block containing both `name:` and `description:` keys) on a `.md`/`.markdown` or untitled skill document, catching renamed standalone skill markdown files (e.g., `incident-skill.md`) without hiding ordinary Markdown knowledge docs that lack skill frontmatter.
  - Rationale: `SKILL.md` is the canonical Agent Skill filename in this project (`server/internal/skills/validator.go`), and checking YAML skill frontmatter handles renamed skill `.md` uploads while avoiding false positives on regular documentation (e.g., `Skill Matrix.pdf` or `Onboarding Guide.md`).
  - Alternatives considered: Backend SQL filtering in `SearchSimilar` was explicitly excluded per user preference for UI-only filtering; hiding all `.md` citations was rejected because legitimate Markdown knowledge base docs must still show citations.
- **Filter at both state ingestion and render boundary**:
  - Apply `filterVisibleCitations` when handling `citation` RTC events in `useVoiceAgentRTC` (and reset `expandedCitation` by stable citation key `<documentId>-<chunkIndex>` rather than array index in `web/src/app/page.tsx`) so filtered arrays never leave stale open indices.
- **Use authenticated role as a render-only gate**:
  - After Agent Skill citations are filtered, render the non-skill citation section and its expandable excerpt previews only when the authenticated `user.role` is `admin`.
  - Keep citation events, transcripts, the top-header `activeTool` badge, and backend RAG retrieval unchanged for non-admin users; role-aware visibility is deliberately a home-page presentation rule.
  - Rationale: the role is already loaded by the home page session check, so no new endpoint or payload field is needed. Filtering on role in the render boundary also prevents citation buttons and excerpt content from entering the DOM for non-admin users.

## Risks / Trade-offs

- [A legitimate user-facing knowledge document literally named `SKILL.md` would be hidden from home-page citation pills] → `SKILL.md` is reserved by convention for Agent Skill playbooks; regular documentation uses descriptive titles/filenames (`Guide.md`, `Policy.pdf`).
- [Subsequent chunks of a renamed skill `.md` file (not named `SKILL.md`) might not include line-1 YAML frontmatter] → Matching both `SKILL.md`/`SKILL.markdown` filenames (all chunks) and YAML `name:` + `description:` frontmatter covers canonical skill files completely and first-chunk frontmatter of custom-named skill files without risking false positives on normal docs.
- [The user role is unavailable briefly while the session request resolves] → Treat the citation section as hidden unless the authenticated role is explicitly `admin`; once the session result loads, administrators regain the permitted citation UI.

# Tasks

## 1. Home-Page Agent Skill Citation Filtering

- [x] 1.1 Add `isAgentSkillCitation` and `filterVisibleCitations` helpers in `web/src/hooks/useVoiceAgentRTC.ts` (matching `SKILL.md` / `SKILL.markdown` titles and Agent Skill YAML frontmatter chunks), apply filtering on incoming `citation` RTC events and in `web/src/app/page.tsx` with stable `<documentId>-<chunkIndex>` expanded-citation keys, and verify the module compiles cleanly.
- [x] 1.2 Add unit and home-page rendering tests in `web/src/__tests__/rtc-hook.test.ts` covering `SKILL.md` chunks hidden when returned alone or mixed with standard knowledge citations while preserving regular `.pdf` / `.md` citations and top-header tool indicators; verify with `npx vitest run src/__tests__/rtc-hook.test.ts` from `web/`.
- [x] 1.3 Gate non-skill RAG citation badges and their expandable excerpts in `web/src/app/page.tsx` to authenticated users whose role is `admin`, while leaving transcripts, responses, media controls, RTC citation payloads, and the active tool indicator unchanged for non-admin users; verify the page type-checks cleanly.
- [x] 1.4 Extend `web/src/__tests__/rtc-hook.test.ts` to verify administrators see non-skill citation badges and excerpt previews, non-admin users see neither the citation section nor excerpt content, Agent Skill citations remain hidden for both roles, and active tool indicators remain visible; verify with `npx vitest run src/__tests__/rtc-hook.test.ts` from `web/`.

## 2. Frontend Verification

- [x] 2.1 Run the full frontend test suite and production build (`npm test` and `npm run build` in `web/`) and confirm zero test, TypeScript, or build regressions.
- [x] 2.2 After the role-aware citation work is complete, rerun the full frontend tests and production build (`npm test` and `npm run build` in `web/`) and confirm zero regressions.

import { describe, expect, it } from "vitest";
import { formatSkillMetadata, getSkillToolReadiness } from "@/lib/agent-skills";

describe("Agent Skills helpers", () => {
  it("reports MCP readiness only when an enabled server allows a hinted tool", () => {
    const servers = [
      { name: "Reports", enabled: true, allowedTools: ["list_reports", "get_report"] },
      { name: "Disabled server", enabled: false, allowedTools: ["delete_report"] },
    ];

    expect(getSkillToolReadiness(["list_reports", "delete_report", "missing", "list_reports"], servers)).toEqual([
      { toolName: "list_reports", status: "ready", serverNames: ["Reports"] },
      { toolName: "delete_report", status: "unconfigured", serverNames: [] },
      { toolName: "missing", status: "unconfigured", serverNames: [] },
    ]);
  });

  it("formats source, reference count, and upload size", () => {
    expect(formatSkillMetadata({
      sourceFormat: "zip",
      referenceFilePaths: ["references/api.md"],
      sizeBytes: 1536,
    })).toEqual({
      sourceLabel: "ZIP bundle",
      referenceFileCount: 1,
      referenceFileLabel: "1 reference file",
      fileSizeLabel: "1.5 KB",
    });

    expect(formatSkillMetadata({
      sourceFormat: "md",
      referenceFilePaths: [],
      sizeBytes: 0,
    })).toEqual({
      sourceLabel: "Markdown",
      referenceFileCount: 0,
      referenceFileLabel: "0 reference files",
      fileSizeLabel: "0 B",
    });
  });
});

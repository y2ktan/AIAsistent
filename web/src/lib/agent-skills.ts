export interface AgentSkill {
  id: string;
  name: string;
  description: string;
  instructions: string;
  referenceFilePaths: string[];
  allowedTools: string[];
  sourceFormat: "md" | "zip";
  filename: string;
  sizeBytes: number;
  sha256Hash: string;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface MCPServerToolAccess {
  name: string;
  enabled: boolean;
  allowedTools: string[];
}

export interface SkillToolReadiness {
  toolName: string;
  status: "ready" | "unconfigured";
  serverNames: string[];
}

export interface SkillMetadata {
  sourceLabel: string;
  referenceFileCount: number;
  referenceFileLabel: string;
  fileSizeLabel: string;
}

export function getSkillToolReadiness(
  allowedTools: string[],
  servers: MCPServerToolAccess[]
): SkillToolReadiness[] {
  return [...new Set(allowedTools.map((tool) => tool.trim()).filter(Boolean))].map((toolName) => {
    const serverNames = servers
      .filter((server) => server.enabled && server.allowedTools.includes(toolName))
      .map((server) => server.name)
      .sort((a, b) => a.localeCompare(b));
    return {
      toolName,
      status: serverNames.length > 0 ? "ready" : "unconfigured",
      serverNames,
    };
  });
}

export function formatSkillMetadata(skill: Pick<AgentSkill, "sourceFormat" | "referenceFilePaths" | "sizeBytes">): SkillMetadata {
  const referenceFileCount = skill.referenceFilePaths?.length ?? 0;
  return {
    sourceLabel: skill.sourceFormat === "zip" ? "ZIP bundle" : "Markdown",
    referenceFileCount,
    referenceFileLabel: `${referenceFileCount} reference ${referenceFileCount === 1 ? "file" : "files"}`,
    fileSizeLabel: formatFileSize(skill.sizeBytes),
  };
}

function formatFileSize(sizeBytes: number): string {
  if (!Number.isFinite(sizeBytes) || sizeBytes <= 0) return "0 B";
  if (sizeBytes < 1024) return `${Math.floor(sizeBytes)} B`;
  if (sizeBytes < 1024 * 1024) return `${(sizeBytes / 1024).toFixed(1)} KB`;
  return `${(sizeBytes / (1024 * 1024)).toFixed(1)} MB`;
}

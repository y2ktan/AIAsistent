import { readText, trustedUrl } from './http.js';

export class McpClient {
  constructor(config) {
    this.name = config.name;
    this.url = trustedUrl(config.url);
    this.token = config.token;
    this.allowedTools = config.allowedTools ?? [];
    this.nextId = 1;
    this.protocol = '2025-03-26';
  }

  async request(method, params, notification = false) {
    const id = notification ? undefined : this.nextId++;
    const response = await fetch(this.url, {
      method: 'POST',
      redirect: 'error',
      signal: AbortSignal.timeout(30_000),
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json, text/event-stream',
        ...(this.token ? { Authorization: 'Bearer ' + this.token } : {}),
        ...(this.session ? { 'Mcp-Session-Id': this.session } : {}),
        ...(method !== 'initialize' ? { 'MCP-Protocol-Version': this.protocol } : {}),
      },
      body: JSON.stringify({ jsonrpc: '2.0', ...(notification ? {} : { id }), method, params }),
    });
    if (!response.ok) {
      await response.body?.cancel();
      throw new Error(`MCP server ${this.name} returned HTTP ${response.status}.`);
    }
    const session = response.headers.get('mcp-session-id');
    if (session && method === 'initialize') this.session = session;
    if (notification) {
      await response.body?.cancel();
      return;
    }
    let message;
    if (response.headers.get('content-type')?.includes('text/event-stream')) {
      // Stop reading as soon as this request's result arrives; SSE may stay open.
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      let size = 0;
      try {
        while (!message) {
          const { value, done } = await reader.read();
          if (done) break;
          size += value.length;
          if (size > 2 * 1024 * 1024) throw new Error('MCP response exceeds the size limit.');
          buffer += decoder.decode(value, { stream: true });
          let boundary;
          while ((boundary = /\r?\n\r?\n/.exec(buffer))) {
            const event = buffer.slice(0, boundary.index);
            buffer = buffer.slice(boundary.index + boundary[0].length);
            const data = event.split(/\r?\n/).filter((line) => line.startsWith('data:'))
              .map((line) => line.slice(5).trimStart()).join('\n');
            if (!data) continue;
            const candidate = JSON.parse(data);
            if (candidate.id === id && ('result' in candidate || 'error' in candidate)) {
              message = candidate;
              break;
            }
            if (candidate.method && candidate.id !== undefined) {
              throw new Error('MCP server requests are not supported by this client.');
            }
          }
        }
      } finally {
        await reader.cancel();
      }
    } else {
      message = JSON.parse(await readText(response.body));
    }
    if (!message || message.jsonrpc !== '2.0' || message.id !== id) {
      throw new Error(`Invalid MCP response from ${this.name}.`);
    }
    if (message.error) throw new Error(`MCP request to ${this.name} failed.`);
    if (!('result' in message)) throw new Error(`Missing MCP result from ${this.name}.`);
    return message.result;
  }

  async connect() {
    const initialized = await this.request('initialize', {
      protocolVersion: this.protocol,
      capabilities: {},
      clientInfo: { name: 'AIAsistent', version: '1.0.0' },
    });
    if (!['2025-03-26', '2025-06-18', '2025-11-25'].includes(initialized?.protocolVersion)) {
      throw new Error(`Unsupported MCP protocol from ${this.name}.`);
    }
    this.protocol = initialized.protocolVersion;
    await this.request('notifications/initialized', {}, true);
    const tools = [];
    let cursor;
    for (let page = 0; page < 10; page++) {
      const result = await this.request('tools/list', cursor ? { cursor } : {});
      if (!Array.isArray(result?.tools)) throw new Error(`Invalid tool list from ${this.name}.`);
      tools.push(...result.tools.filter((tool) => this.allowedTools.includes(tool.name)));
      if (tools.length > 100) throw new Error('Too many MCP tools.');
      cursor = result.nextCursor;
      if (!cursor) return tools;
    }
    throw new Error('MCP tool pagination limit exceeded.');
  }

  async call(name, args) {
    if (!this.allowedTools.includes(name)) throw new Error('MCP tool is not allowed.');
    return this.request('tools/call', { name, arguments: args });
  }

  async close() {
    if (!this.session) return;
    try {
      const response = await fetch(this.url, {
        method: 'DELETE',
        redirect: 'error',
        signal: AbortSignal.timeout(5000),
        headers: {
          'Mcp-Session-Id': this.session,
          'MCP-Protocol-Version': this.protocol,
          ...(this.token ? { Authorization: 'Bearer ' + this.token } : {}),
        },
      });
      await response.body?.cancel();
    } catch {
      // Session cleanup is best effort and must not hide a completed answer.
    }
  }
}

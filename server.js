import http from 'node:http';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { DocumentStore } from './lib/documents.js';
import { chat, validateMessages } from './lib/chat.js';
import { readText, trustedUrl } from './lib/http.js';

export function loadConfig(env = process.env) {
  const mcpServers = JSON.parse(env.MCP_SERVERS ?? '[]');
  if (!Array.isArray(mcpServers) || mcpServers.length > 5) throw new Error('MCP_SERVERS must be an array of up to 5 servers.');
  for (const server of mcpServers) {
    if (!server || typeof server.name !== 'string' || !server.name
        || !Array.isArray(server.allowedTools) || server.allowedTools.some((name) => typeof name !== 'string')
        || (server.token !== undefined && typeof server.token !== 'string')) {
      throw new Error('Each MCP server requires name, url, and an allowedTools array.');
    }
    trustedUrl(server.url);
  }
  const baseUrl = env.AI_BASE_URL ?? 'https://api.openai.com/v1';
  trustedUrl(baseUrl);
  return { apiKey: env.AI_API_KEY, baseUrl, model: env.AI_MODEL ?? 'gpt-4o-mini', mcpServers };
}

export function createApp(config = loadConfig(), documents = new DocumentStore()) {
  let busy = false;
  return http.createServer(async (request, response) => {
    response.setHeader('X-Content-Type-Options', 'nosniff');
    response.setHeader('Content-Security-Policy', "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'");
    response.setHeader('Cache-Control', 'no-store');
    const send = (status, value) => {
      response.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8' });
      response.end(JSON.stringify(value));
    };
    // Host and Origin checks protect a localhost service from DNS rebinding and CSRF.
    const host = request.headers.host;
    if (!host || !/^(127\.0\.0\.1|localhost):\d+$/.test(host)
        || (request.headers.origin && request.headers.origin !== `http://${host}`)) {
      return send(403, { error: 'Only same-origin localhost requests are allowed.' });
    }
    let path;
    try {
      path = new URL(request.url, `http://${host}`).pathname;
    } catch {
      return send(400, { error: 'Invalid request URL.' });
    }
    try {
      if (request.method === 'GET' && ['/', '/app.js', '/style.css'].includes(path)) {
        const file = path === '/' ? 'index.html' : path.slice(1);
        const content = await readFile(new URL(`./public/${file}`, import.meta.url));
        response.writeHead(200, { 'Content-Type': {
          'index.html': 'text/html; charset=utf-8',
          'app.js': 'text/javascript; charset=utf-8',
          'style.css': 'text/css; charset=utf-8',
        }[file] });
        return response.end(content);
      }
      if (request.method === 'GET' && path === '/api/status') {
        return send(200, {
          configured: Boolean(config.apiKey), model: config.model,
          mcpServers: config.mcpServers.map(({ name, allowedTools }) => ({ name, allowedTools })),
        });
      }
      if (request.method === 'GET' && path === '/api/documents') return send(200, documents.list());
      if (!['POST', 'DELETE'].includes(request.method) || !path.startsWith('/api/')) {
        return send(404, { error: 'Not found.' });
      }
      if (request.headers['x-assistant-request'] !== '1'
          || !request.headers['content-type']?.startsWith('application/json')) {
        return send(403, { error: 'Expected a same-origin JSON API request.' });
      }
      let body;
      try {
        body = JSON.parse(await readText(request, 2 * 1024 * 1024));
      } catch {
        return send(400, { error: 'Invalid JSON or request exceeds 2 MiB.' });
      }
      if (!body || typeof body !== 'object' || Array.isArray(body)) return send(400, { error: 'Expected a JSON object.' });
      if (request.method === 'POST' && path === '/api/documents') {
        try {
          return send(201, documents.add(body.name, body.text));
        } catch (error) {
          return send(400, { error: error.message });
        }
      }
      if (request.method === 'DELETE' && path.startsWith('/api/documents/')) {
        return documents.remove(path.slice('/api/documents/'.length))
          ? send(200, { removed: true }) : send(404, { error: 'Document not found.' });
      }
      if (request.method === 'POST' && path === '/api/chat') {
        try {
          validateMessages(body.messages);
          if (body.useTools !== undefined && typeof body.useTools !== 'boolean') throw new Error('useTools must be a boolean.');
        } catch (error) {
          return send(400, { error: error.message });
        }
        if (!config.apiKey) return send(503, { error: 'Set AI_API_KEY on the server to enable chat.' });
        if (busy) return send(429, { error: 'A chat request is already running. Please wait.' });
        busy = true;
        try {
          return send(200, await chat(config, documents, body.messages, body.useTools === true));
        } catch {
          // Do not echo provider/MCP error bodies, which may contain credentials.
          return send(502, { error: 'Chat failed. Check the AI/MCP configuration, provider availability, and tool permissions; then retry.' });
        } finally {
          busy = false;
        }
      }
      return send(404, { error: 'Not found.' });
    } catch {
      return send(500, { error: 'Unable to process request.' });
    }
  });
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const port = Number(process.env.PORT ?? 3000);
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('PORT must be between 1 and 65535.');
  createApp().listen(port, '127.0.0.1', () => {
    console.log(`AIAsistent is available at http://127.0.0.1:${port}`);
  });
}

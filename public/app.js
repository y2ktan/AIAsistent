const $ = (id) => document.getElementById(id);
let history = [];
let waiting = false;

async function api(path, method = 'GET', body) {
  const response = await fetch(path, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Assistant-Request': '1' },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  });
  const result = await response.json();
  if (!response.ok) throw new Error(result.error ?? 'Request failed.');
  return result;
}

function showError(error) {
  $('error').textContent = error?.message ?? '';
}

function addMessage(role, text, sources = [], usedTools = []) {
  const article = document.createElement('article');
  article.className = role;
  const heading = document.createElement('strong');
  heading.textContent = role === 'user' ? 'You' : 'Assistant';
  const content = document.createElement('p');
  content.textContent = text;
  article.append(heading, content);
  if (sources.length) {
    const details = document.createElement('details');
    const summary = document.createElement('summary');
    summary.textContent = 'Retrieved sources';
    details.append(summary);
    for (const source of sources) {
      const excerpt = document.createElement('p');
      excerpt.textContent = `[${source.citation}] ${source.name}, lines ${source.startLine}–${source.endLine}\n${source.text}`;
      details.append(excerpt);
    }
    article.append(details);
  }
  if (usedTools.length) {
    const info = document.createElement('p');
    info.textContent = `Tools used: ${usedTools.join(', ')}`;
    article.append(info);
  }
  $('messages').append(article);
  article.scrollIntoView({ block: 'nearest' });
}

async function refreshDocuments() {
  const documents = await api('/api/documents');
  $('documents').replaceChildren();
  for (const documentInfo of documents) {
    const item = document.createElement('li');
    item.append(`${documentInfo.name} (${documentInfo.chunks} chunks) `);
    const remove = document.createElement('button');
    remove.textContent = 'Remove';
    remove.setAttribute('aria-label', `Remove ${documentInfo.name}`);
    remove.onclick = async () => {
      try {
        await api(`/api/documents/${documentInfo.id}`, 'DELETE', {});
        await refreshDocuments();
      } catch (error) { showError(error); }
    };
    item.append(remove);
    $('documents').append(item);
  }
}

$('upload').onchange = async () => {
  showError();
  try {
    for (const file of $('upload').files) {
      if (file.size > 1024 * 1024) throw new Error(`${file.name} exceeds 1 MiB.`);
      await api('/api/documents', 'POST', { name: file.name, text: await file.text() });
    }
  } catch (error) {
    showError(error);
  } finally {
    $('upload').value = '';
    await refreshDocuments().catch(showError);
  }
};

$('chat-form').onsubmit = async (event) => {
  event.preventDefault();
  const content = $('prompt').value.trim();
  if (!content || waiting) return;
  waiting = true;
  $('send').disabled = true;
  $('clear').disabled = true;
  showError();
  const messages = [...history.slice(-38), { role: 'user', content }];
  addMessage('user', content);
  $('prompt').value = '';
  try {
    const answer = await api('/api/chat', 'POST', { messages, useTools: $('tools').checked });
    history = [...messages, { role: 'assistant', content: answer.reply.slice(0, 8000) }];
    addMessage('assistant', answer.reply, answer.sources, answer.usedTools);
  } catch (error) {
    $('messages').lastElementChild?.remove();
    $('prompt').value = content;
    showError(error);
  } finally {
    waiting = false;
    $('send').disabled = false;
    $('clear').disabled = false;
    $('prompt').focus();
  }
};

$('clear').onclick = () => {
  history = [];
  $('messages').replaceChildren();
  $('tools').checked = false;
  showError();
};

async function initialize() {
  const status = await api('/api/status');
  $('status').textContent = status.configured ? `Ready — model: ${status.model}`
    : 'Chat is not configured. Set AI_API_KEY on the server and restart.';
  const allowed = status.mcpServers.flatMap((server) =>
    server.allowedTools.map((tool) => `${server.name}/${tool}`));
  $('tool-info').textContent = allowed.length
    ? `Available tools: ${allowed.join(', ')}. Enabling tools authorizes AI-selected calls; they may have side effects.`
    : 'No MCP tools configured. See README for server setup.';
  $('tools').disabled = !allowed.length;
  await refreshDocuments();
}
initialize().catch(showError);

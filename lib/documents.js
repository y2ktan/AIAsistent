import { randomUUID } from 'node:crypto';

function terms(text) {
  return text.toLowerCase().match(/[\p{L}\p{N}]{2,}/gu) ?? [];
}

export class DocumentStore {
  #documents = new Map();

  list() {
    return [...this.#documents.values()].map(({ id, name, chunks }) => ({
      id, name, chunks: chunks.length,
    }));
  }

  add(name, text) {
    if (typeof name !== 'string' || name.length > 200 || !/\.(txt|md)$/i.test(name)) {
      throw new Error('Use a .txt or .md filename (up to 200 characters).');
    }
    if (typeof text !== 'string' || !text.trim() || text.includes('\0')
        || Buffer.byteLength(text) > 1024 * 1024) {
      throw new Error('Documents must contain non-empty text, up to 1 MiB.');
    }
    if (this.#documents.size >= 20) throw new Error('Remove a document before adding more (limit: 20).');
    const lines = text.replace(/\r\n?/g, '\n').split('\n');
    const chunks = [];
    let content = '';
    let startLine = 1;
    const flush = (endLine) => {
      if (content.trim()) chunks.push({
        text: content.trim(), startLine, endLine, words: new Set(terms(content)),
      });
      content = '';
    };
    lines.forEach((line, index) => {
      if (content && content.length + line.length > 1600) {
        flush(index);
        startLine = index + 1;
      }
      // Split very long lines as well, retaining their source line for citations.
      for (let offset = 0; offset < Math.max(line.length, 1); offset += 1600) {
        content += `${line.slice(offset, offset + 1600)}\n`;
        if (content.length >= 1600) {
          flush(index + 1);
          startLine = index + 1;
        }
      }
      if (!content) startLine = index + 2;
    });
    flush(lines.length);
    const id = randomUUID();
    this.#documents.set(id, { id, name, chunks });
    return this.list().find((document) => document.id === id);
  }

  remove(id) {
    return this.#documents.delete(id);
  }

  retrieve(query, limit = 4) {
    const queryTerms = [...new Set(terms(query))];
    const chunks = [...this.#documents.values()].flatMap(({ id, name, chunks }) =>
      chunks.map((chunk) => ({ ...chunk, id, name })));
    const frequency = new Map(queryTerms.map((term) =>
      [term, chunks.filter(({ words }) => words.has(term)).length]));
    return chunks.map((chunk) => ({
      ...chunk,
      score: queryTerms.reduce((score, term) => score + (chunk.words.has(term)
        ? Math.log(1 + chunks.length / (1 + frequency.get(term))) : 0), 0),
    })).filter(({ score }) => score > 0)
      .sort((a, b) => b.score - a.score)
      .slice(0, limit)
      .map(({ name, id, text, startLine, endLine }, index) => ({
        citation: index + 1, name, id, text, startLine, endLine,
      }));
  }
}

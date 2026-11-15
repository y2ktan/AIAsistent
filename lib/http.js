export async function readText(stream, limit = 2 * 1024 * 1024) {
  const chunks = [];
  let size = 0;
  const iterator = typeof stream.iterator === 'function'
    ? stream.iterator({ destroyOnReturn: false }) : stream;
  for await (const chunk of iterator) {
    size += chunk.length;
    if (size > limit) {
      // Drain oversized incoming requests without destroying the response socket.
      stream.resume?.();
      throw new Error('Response or request exceeds the size limit.');
    }
    chunks.push(Buffer.from(chunk));
  }
  return Buffer.concat(chunks).toString('utf8');
}

export function trustedUrl(value) {
  const url = new URL(value);
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.hash) {
    throw new Error('Configured endpoints must be HTTP(S) URLs without credentials or fragments.');
  }
  return url;
}

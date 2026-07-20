// api.js — every /api/* call in one place: this file is the browser-side
// contract with internal/webui's handlers (one function per route). Pages
// talk to the backend only through here, so a new endpoint means one
// function here plus its handler on the Go side. The streaming endpoints
// (/api/stream, /api/confirm) are the deliberate exception — they live in
// stream.js because the SSE transport is inseparable from chunk handling.
//
// GET helpers return parsed JSON; mutating helpers return the raw
// Response, because their callers branch on res.ok / statusText.

function getJSON(url) {
  return fetch(url).then(r => r.json());
}

function postJSON(url, body) {
  return fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

export const getStatus     = ()  => getJSON('/api/status');
export const getThemes     = ()  => getJSON('/api/themes');
export const getSettings   = ()  => getJSON('/api/settings');
export const listSessions  = ()  => getJSON('/api/sessions');
export const getTranscript = id  => getJSON('/api/transcript/' + id);
export const listDir       = dir => getJSON('/api/files?dir=' + encodeURIComponent(dir));

export const newSession    = ()  => fetch('/api/sessions', { method: 'POST' }).then(r => r.json());
export const deleteSession = id  => fetch('/api/sessions/' + id, { method: 'DELETE' });
export const postInterrupt = ()  => fetch('/api/interrupt', { method: 'POST' });

export const getAgents     = ()   => fetch('/api/agents');
export const putAgent      = body => postJSON('/api/agents', body);
export const putKey        = (provider, key) => postJSON('/api/key', { provider, key });
export const putSettings   = s    => postJSON('/api/settings', s);

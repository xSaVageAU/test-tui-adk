// stream.js — the streaming turn lifecycle: send a message over SSE
// (/api/stream via EventSource), translate StreamChunks into store
// mutations (handleChunk), resume after a HITL confirmation (/api/confirm,
// which streams the rest of the turn over a POST body), and interrupt.
// These two endpoints deliberately bypass api.js: their transport IS the
// logic here, not a detail behind it.

import { A, loadStatus, updateContextBar } from './store.js';
import { setStreaming, setWorkingLabel, stopSpinning } from './anim.js';
import { refreshFileTree } from './filetree.js';
import { postInterrupt } from './api.js';

let evtSrc = null;

export function sendMessage(text) {
  if (A().isStreaming || !text.trim()) return;
  document.getElementById('chat-input').value = '';
  A().closePalette();
  A().pushUserMsg(text);
  setStreaming(true, 'thinking');

  const url = `/api/stream?message=${encodeURIComponent(text)}&sessionId=${encodeURIComponent(A().sessionId)}`;
  evtSrc = new EventSource(url);
  let rStart = null;
  const rs = { get: () => rStart, set: v => { rStart = v; } };
  evtSrc.onmessage = e => { try { handleChunk(JSON.parse(e.data), rs); } catch(ex) { console.error(ex); } };
  evtSrc.onerror   = ()  => finishStream();
}

function handleChunk(c, rs) {
  if (c.Err) { A().sysMsg('Error: ' + c.Err); finishStream(); return; }

  if (c.Reasoning) {
    if (!rs.get()) rs.set(Date.now());
    A().appendReasoning(c.Reasoning);
  }
  if (c.Text) {
    if (rs.get()) { A().appendReasoning('', Date.now() - rs.get()); rs.set(null); }
    A().appendText(c.Text);
    setWorkingLabel('thinking');
  }
  if (c.ToolCall) {
    if (rs.get()) { A().appendReasoning('', Date.now() - rs.get()); rs.set(null); }
    A().pushToolCall(c.ToolCall.ID, c.ToolCall.Name, c.ToolCall.Args);
    setWorkingLabel('using ' + c.ToolCall.Name);
  }
  if (c.ToolResult) {
    A().updateToolResult(c.ToolResult.ID, c.ToolResult.Result);
    setWorkingLabel('thinking');
    // A finished tool may have touched the filesystem — refresh the
    // sidebar right away instead of waiting out the poll interval. The
    // JSON compare in refreshFileTree makes a no-op result free.
    if (A().filetreeOpen) refreshFileTree();
  }
  if (c.Confirmation) {
    if (rs.get()) { A().appendReasoning('', Date.now() - rs.get()); rs.set(null); }
    const conf = c.Confirmation;
    A().pendingToolConfirm(conf.OriginalID, conf.ID, conf.Hint);
    // TUI parity: the anim stops (blank reserved rows) while a HITL
    // confirmation waits on the user — isStreaming stays true so the
    // input stays gated, matching turnInProgress.
    stopSpinning();
    finishStream(true); // pause — HITL needs user action
  }
  if (c.FinishReason && c.FinishReason !== '') A().closeAgentBubble();
  if (c.Usage) updateContextBar(c.Usage.Total, A().contextWindow);
  if (c.ReloadRequested) loadStatus();
}

/**
 * End the current stream.
 * Always resets agentMsgIdx — this is the core fix for responses
 * appending to the previous message.
 */
export function finishStream(keepStreaming) {
  if (evtSrc) { evtSrc.close(); evtSrc = null; }
  A().closeAgentBubble(); // ← always reset; prevents next response re-using old bubble
  if (!keepStreaming) setStreaming(false);
  document.getElementById('chat-input')?.focus();
}

export async function interrupt() {
  try { await postInterrupt(); } catch {}
  finishStream();
  A().sysMsg('Interrupted.');
}

// ══════════════════════════════════════════════════════════════════
//  HITL confirmation (reached from the tool row's approve/deny buttons)
// ══════════════════════════════════════════════════════════════════
export async function doConfirm(confirmId, origId, approved) {
  A().resolveToolConfirm(origId, approved);
  setStreaming(true, 'thinking'); // matches hitl.go's resolveConfirmation
  try {
    const res = await fetch('/api/confirm', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId: A().sessionId, decisions: [{ ID: confirmId, Approved: approved }] }),
    });
    if (!res.ok) { A().sysMsg('Confirm failed: ' + res.statusText); setStreaming(false); return; }

    let rStart = null;
    const rs = { get: () => rStart, set: v => { rStart = v; } };
    const reader = res.body.getReader(); const dec = new TextDecoder(); let buf = '';
    for (;;) {
      const { value, done } = await reader.read(); if (done) break;
      buf += dec.decode(value, { stream: true });
      const parts = buf.split('\n\n'); buf = parts.pop();
      for (const p of parts) {
        const line = p.trim();
        if (line.startsWith('data: ')) try { handleChunk(JSON.parse(line.slice(6)), rs); } catch {}
      }
    }
  } catch (e) { A().sysMsg('Error: ' + e.message); }
  finishStream();
}

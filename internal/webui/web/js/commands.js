// commands.js — the slash-command registry (mirrors commandSpecs in
// internal/ui/commands.go), the cmd* implementations, and the modal
// confirm/navigation logic they share. Template-facing entry points
// (modalClose, modalConfirm, modalHover, selectPaletteItem, submitKey,
// submitAgentDetail) are exported here and put on window by main.js.

import { A, showNotice, loadStatus, saveSettings } from './store.js';
import { applyTheme } from './theme.js';
import { setLoader, startSpinning, LOADER_NAMES } from './anim.js';
import { interrupt } from './stream.js';
import { refreshFileTree } from './filetree.js';
import { onOff, fmtDate, shortId, scrollBottom } from './format.js';
import * as api from './api.js';

export const COMMANDS = [
  { name: '/new',           desc: 'Start a new session' },
  { name: '/sessions',      desc: 'Switch to a past session' },
  { name: '/theme',         desc: 'Change the color theme' },
  { name: '/settings',      desc: 'Adjust settings' },
  { name: '/key',           desc: 'Set a provider API key' },
  { name: '/agents',        desc: 'Configure agent provider/model' },
  { name: '/loader',        desc: 'Choose the "working" animation' },
  { name: '/interrupt',     desc: 'Stop the current response' },
  { name: '/reload-agents', desc: 'Reload agents/tools/MCP servers from disk' },
  { name: '/exit',          desc: 'Quit the app' },
];

export function runCommand(name) {
  switch (name) {
    case '/new':           cmdNew();          break;
    case '/sessions':      cmdSessions();     break;
    case '/theme':         cmdTheme();        break;
    case '/settings':      cmdSettings();     break;
    case '/key':           cmdKey();          break;
    case '/agents':        cmdAgents();       break;
    case '/loader':        cmdLoader();       break;
    case '/interrupt':     interrupt();       break;
    case '/reload-agents': cmdReloadAgents(); break;
    case '/exit':          window.close();    break;
    default: A().sysMsg('Unknown command: ' + name); break;
  }
}

async function cmdNew() {
  try {
    const d = await api.newSession();
    A().sessionId  = d.sessionId;
    A().sessionTag = shortId(d.sessionId);
    A().clearMessages();
    showNotice('Started a new session.');
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

async function cmdSessions() {
  try {
    const list = await api.listSessions();
    if (!list?.length) { showNotice('No past sessions yet.'); return; }
    A().openModal({
      kind:  'sessions',
      title: 'Choose session',
      items: list.map(s => ({
        id:  s.ID,
        name: shortId(s.ID),
        tag:  s.ID === A().sessionId ? 'current' : fmtDate(s.UpdatedAt),
      })),
      idx: Math.max(0, list.findIndex(s => s.ID === A().sessionId)),
    });
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

function cmdTheme() {
  if (!A().themes.length) { A().sysMsg('No themes available.'); return; }
  const origin = A().currentTheme;
  A().openModal({
    kind:  'theme',
    title: 'Choose theme',
    items: A().themes.map(t => ({ id: t.name, name: t.name, tag: t.name === origin ? 'current' : '' })),
    idx:   Math.max(0, A().themes.findIndex(t => t.name === origin)),
    data:  { origin },
  });
}

function cmdSettings() {
  A().openModal({
    kind:  'settings',
    title: 'Settings',
    items: [
      { id: 'tui',   name: 'TUI Settings',  tag: 'display, tool output' },
      { id: 'agent', name: 'Agent Settings', tag: 'tool approval policy' },
    ],
  });
}

function buildTUIItems() {
  const s = A().settings;
  return [
    { id: 'highlight', name: 'Highlight user messages',      tag: onOff(A().highlightUser) },
    { id: 'stream',    name: 'Stream replies token-by-token', tag: onOff(s?.UI?.StreamReplies ?? true) },
    { id: 'verbose',   name: 'Verbose tool output',          tag: onOff(A().verboseTools) },
    { id: 'reasoning', name: 'Show reasoning text',          tag: onOff(A().showReasoning) },
  ];
}

function openTUISettings() {
  A().openModal({ kind: 'settings-tui', title: 'TUI Settings', items: buildTUIItems(), back: cmdSettings });
}

function cmdLoader() {
  if (!A().settings) { A().sysMsg('Settings not loaded.'); return; }
  const origin = A().settings.UI?.WorkingAnim || "Equalizer";
  A().openModal({
    kind:  'loader',
    title: 'Choose animation',
    items: LOADER_NAMES.map(name => ({ id: name, name: name, tag: name === origin ? 'current' : '' })),
    idx:   Math.max(0, LOADER_NAMES.indexOf(origin)),
    data:  { origin },
  });
  // Live preview while the picker is open, whether or not a turn is
  // running — same as the TUI's workingAnimShouldRun (paletteLoader).
  startSpinning();
}

function openAgentSettings() {
  const items = [
    { id: 'permission', name: 'Tool approval mode', tag: modeTag() }
  ];
  if (A().settings?.Agent?.Target) {
    items.push({ id: 'target', name: 'Tool execution target', tag: targetTag() });
  }
  A().openModal({
    kind:  'settings-agent',
    title: 'Agent Settings',
    items: items,
    back: cmdSettings,
  });
}
function modeTag() { return (A().settings?.Agent?.PermissionMode || 'normal') + ' — select to cycle'; }
function targetTag() {
  const t = A().settings?.Agent?.Target?.Type || 'host';
  const desc = A().targetDescription || 'local host';
  return t + ' (' + desc + ') — select to cycle';
}

function cmdKey() { A().openModal({ kind: 'key', title: 'Set API key', items: [] }); }

async function cmdAgents() {
  try {
    const res = await api.getAgents();
    if (!res.ok) { A().sysMsg('Agents not supported: ' + res.statusText); return; }
    A().agentsData = await res.json();
    A().openModal({
      kind:  'agents',
      title: 'Agents',
      items: (A().agentsData.agents || []).map(ag => ({
        id:   ag.ID,
        name: ag.Name || (ag.IsRoot ? 'Root Agent' : ag.ID),
        tag:  (ag.Provider || 'gemini') + ' / ' + (ag.Model || 'default'),
      })),
    });
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

async function cmdReloadAgents() {
  showNotice('Reloading agents...');
  try {
    const res = await api.putAgent({ id: '', provider: '', model: '', tools: [] });
    if (res.ok) { showNotice('Agents reloaded.'); await loadStatus(); }
    else A().sysMsg('Reload failed: ' + res.statusText);
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

// ══════════════════════════════════════════════════════════════════
//  Modal confirmation logic
// ══════════════════════════════════════════════════════════════════

export function modalClose() { A().closeModal(true); }

export function modalHover(i) {
  if (!A().modal || A().modal.idx === i) return;
  A().modal.idx = i;
  if (A().modal.kind === 'theme')  applyTheme(A().modal.items[i].id, false);
  if (A().modal.kind === 'loader') setLoader(A().modal.items[i].id, false);
}

/**
 * Confirm the currently-selected (or a clicked) modal item.
 * i is optional — if supplied, modal.idx is updated first.
 */
export function modalConfirm(i) {
  if (!A().modal) return;
  if (i !== undefined) A().modal.idx = i;
  const item = A().modal.items[A().modal.idx];
  if (!item && A().modal.kind !== 'key' && A().modal.kind !== 'agent-detail') return;
  doModalConfirm(item);
}

function doModalConfirm(item) {
  const kind = A().modal?.kind;
  switch (kind) {
    case 'theme':
      if (item) {
        A().modal.data = null; // commit — don't revert on close
        applyTheme(item.id, true);
        A().closeModal(false);
      }
      break;

    case 'sessions':
      if (item) switchSession(item.id);
      break;

    case 'settings':
      if (!item) break;
      A().closeModal(false);
      // setTimeout defers the sub-menu open so there's no keydown
      // event bubbling into the freshly-opened modal.
      if (item.id === 'tui')   setTimeout(openTUISettings,   0);
      if (item.id === 'agent') setTimeout(openAgentSettings, 0);
      break;

    case 'settings-tui':
      if (item) { toggleTUISetting(item.id); A().modal.items = buildTUIItems(); }
      break;

    case 'settings-agent':
      if (item) toggleAgentSetting(item.id);
      break;

    case 'loader':
      if (item) {
        A().modal.data = null; // commit
        setLoader(item.id, true);
        A().closeModal(false);
      }
      break;

    case 'agents':
      if (item) openAgentDetail(item.id);
      break;
  }
}

function toggleTUISetting(id) {
  const s = A().settings; if (!s) return;
  switch (id) {
    case 'highlight': A().highlightUser = !A().highlightUser; s.UI.HighlightUser = A().highlightUser; break;
    case 'stream':    s.UI.StreamReplies = !s.UI.StreamReplies; break;
    case 'verbose':   A().verboseTools = !A().verboseTools; s.UI.VerboseTools = A().verboseTools; break;
    case 'reasoning': A().showReasoning = !A().showReasoning; s.UI.HideReasoningText = !A().showReasoning; break;
  }
  saveSettings();
}

function toggleAgentSetting(id) {
  const s = A().settings; if (!s) return;
  if (id === 'permission') {
    s.Agent.PermissionMode = s.Agent.PermissionMode === 'normal' ? 'full-auto' : 'normal';
    A().autoAccept = s.Agent.PermissionMode === 'full-auto';
    saveSettings();
    A().modal.items[0].tag = modeTag();
  } else if (id === 'target') {
    s.Agent.Target.Type = s.Agent.Target.Type === 'host' ? 'ssh' : 'host';
    saveSettingsAndReconfigure();
  }
}

async function saveSettingsAndReconfigure() {
  await saveSettings();
  await loadStatus(); // loadStatus's ConfigureTarget call installs the new target
  refreshFileTree();  // ...so the sidebar re-roots at the new cwd immediately
  if (A().modal?.kind === 'settings-agent') {
    A().modal.items = [
      { id: 'permission', name: 'Tool approval mode', tag: modeTag() },
      { id: 'target', name: 'Tool execution target', tag: targetTag() }
    ];
  }
}

async function switchSession(id) {
  A().closeModal(false);
  if (id === A().sessionId) return;
  A().sessionId  = id;
  A().sessionTag = shortId(id);
  A().clearMessages();
  showNotice('Switched to ' + shortId(id) + ' — loading history...');
  try {
    const entries = await api.getTranscript(id);
    replayTranscript(entries);
    showNotice('Switched to ' + shortId(id) + (entries?.length ? ' — history loaded.' : '.'));
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

function replayTranscript(entries) {
  for (const e of entries) {
    if (e.UserText)    A().pushUserMsg(e.UserText);
    else if (e.Text)   A().appendText(e.Text);
    else if (e.Reasoning) A().appendReasoning(e.Reasoning, e.ReasoningMs || 0);
    else if (e.ToolCall)  A().pushToolCall(e.ToolCall.ID, e.ToolCall.Name, e.ToolCall.Args);
    else if (e.ToolResult) A().updateToolResult(e.ToolResult.ID, e.ToolResult.Result);
  }
  A().closeAgentBubble();
  Alpine.nextTick(scrollBottom);
}

function openAgentDetail(agentId) {
  const agent = (A().agentsData?.agents || []).find(a => a.ID === agentId);
  if (!agent) return;
  A().openModal({
    kind:  'agent-detail',
    title: agent.Name || agentId,
    items: [],
    data:  { agentId, agent, tools: A().agentsData?.tools || [] },
    back:  cmdAgents,
  });
}

export async function submitKey() {
  const provider = document.getElementById('mf-provider')?.value || 'gemini';
  const key      = document.getElementById('mf-key')?.value?.trim() || '';
  if (!key) { A().sysMsg('Key cannot be empty.'); return; }
  try {
    const res = await api.putKey(provider, key);
    if (res.ok) {
      A().closeModal(false);
      showNotice('API key saved for ' + provider + '.');
      await loadStatus();
    } else {
      const d = await res.json().catch(() => ({}));
      A().sysMsg('Key error: ' + (d.error || res.statusText));
    }
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

export async function submitAgentDetail() {
  const id       = A().modal?.data?.agentId;
  const provider = document.getElementById('mf-aprov')?.value  || '';
  const model    = document.getElementById('mf-amodel')?.value || '';
  const tools    = Array.from(document.querySelectorAll('#modal-body input[type=checkbox]:checked')).map(b => b.value);
  try {
    const res = await api.putAgent({ id, provider, model, tools });
    A().closeModal(false);
    if (res.ok) { showNotice('Agent config saved. Backend rebuilding...'); await loadStatus(); }
    else A().sysMsg('Save failed: ' + res.statusText);
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

export async function modalDeleteSession() {
  if (A().modal?.kind !== 'sessions') return;
  const item = A().modal.items[A().modal.idx];
  if (!item || !confirm('Delete session ' + item.name + '?')) return;
  try {
    await api.deleteSession(item.id);
    if (item.id === A().sessionId) await cmdNew();
    else await cmdSessions();
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

// ══════════════════════════════════════════════════════════════════
//  Command palette selection (clicked item)
// ══════════════════════════════════════════════════════════════════
export function selectPaletteItem(i) {
  const cmd = A().paletteMatches[i]; if (!cmd) return;
  document.getElementById('chat-input').value = '';
  A().closePalette();
  setTimeout(() => runCommand(cmd.name), 0);
}

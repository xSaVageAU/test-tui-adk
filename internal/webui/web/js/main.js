// main.js — entry point. Imports the module graph (which registers the
// Alpine store via store.js's alpine:init listener — this module tag must
// stay ahead of the Alpine CDN tag in index.html), wires the input box
// and global keyboard shortcuts, and publishes the template-facing API.
//
// window exports: index.html's Alpine expressions can only see globals,
// and module top-level declarations are NOT window properties (unlike the
// old single-file script). Everything a template calls must therefore
// appear in the Object.assign(window, …) block below — that block is the
// complete, deliberate contract between the templates and the JS.

import { A, loadStatus, loadSettings, saveSettings, checkNoticeFit } from './store.js';
import { buildBootArt, fmtMs, renderMarkdown } from './format.js';
import { loadThemes } from './theme.js';
import { sendMessage, interrupt, doConfirm } from './stream.js';
import {
  runCommand, modalClose, modalConfirm, modalHover, modalDeleteSession,
  selectPaletteItem, submitKey, submitAgentDetail,
} from './commands.js';
import { toggleFiletree, ftToggle } from './filetree.js';
import { formatToolArgs, formatToolResult, effectiveToolPreviewLines } from './toolformat.js';

// ══════════════════════════════════════════════════════════════════
//  Prompt navigation (pgup / pgdn)
// ══════════════════════════════════════════════════════════════════
function scrollPrompt(dir) {
  const ids = A().userMsgIds;
  if (!ids.length) return;
  const ca = document.getElementById('chat-area');
  if (dir < 0) {
    A().promptScrollPos = A().promptScrollPos <= 0 ? ids.length - 1 : A().promptScrollPos - 1;
  } else {
    if (A().promptScrollPos < 0 || A().promptScrollPos >= ids.length - 1) {
      A().promptScrollPos = -1; ca.scrollTop = ca.scrollHeight; return;
    }
    A().promptScrollPos++;
  }
  const msgId = ids[A().promptScrollPos];
  Alpine.nextTick(() => {
    document.querySelector(`[data-msgid="${msgId}"]`)
      ?.scrollIntoView({ block: 'start', behavior: 'smooth' });
  });
}

// ══════════════════════════════════════════════════════════════════
//  Input wiring
// ══════════════════════════════════════════════════════════════════
function wireInput() {
  const inp = document.getElementById('chat-input');

  inp.addEventListener('input', () => A().updatePalette(inp.value));

  inp.addEventListener('keydown', e => {
    // ── Palette navigation ──────────────────────────────────
    if (A().paletteVisible) {
      if (e.key === 'ArrowUp')   { e.preventDefault(); A().paletteMove(-1); return; }
      if (e.key === 'ArrowDown') { e.preventDefault(); A().paletteMove(+1); return; }
      if (e.key === 'Escape')    { e.preventDefault(); inp.value = ''; A().closePalette(); return; }
      if (e.key === 'Tab')       {
        e.preventDefault();
        const cmd = A().paletteMatches[A().paletteIdx];
        if (cmd) { inp.value = cmd.name; A().closePalette(); }
        return;
      }
      if (e.key === 'Enter') {
        e.preventDefault();
        const cmd = A().paletteMatches[A().paletteIdx];
        if (cmd) {
          inp.value = '';
          A().closePalette();
          // setTimeout defers command so this keydown doesn't reach the modal
          setTimeout(() => runCommand(cmd.name), 0);
        }
        return;
      }
    }

    // ── Send / run command ──────────────────────────────────
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      const val = inp.value.trim();
      if (!val) return;
      if (val.startsWith('/')) {
        inp.value = '';
        A().closePalette();
        const cmdName = '/' + val.slice(1).split(/\s+/)[0].toLowerCase();
        setTimeout(() => runCommand(cmdName), 0);
      } else {
        sendMessage(val);
      }
    }
  });
}

// ══════════════════════════════════════════════════════════════════
//  Global keyboard shortcuts
// ══════════════════════════════════════════════════════════════════
function wireKeyboard() {
  document.addEventListener('keydown', e => {

    // ── Modal keyboard navigation ───────────────────────────
    if (A().modalVisible) {
      if (e.key === 'Escape') { e.preventDefault(); A().closeModal(true); return; }

      const kind = A().modal?.kind;
      // key / agent-detail handle their own keyboard inside Alpine templates
      if (kind === 'key' || kind === 'agent-detail') return;

      if (e.key === 'ArrowDown') { e.preventDefault(); A().modalMove(+1); return; }
      if (e.key === 'ArrowUp')   { e.preventDefault(); A().modalMove(-1); return; }
      if (e.key === 'Enter') {
        e.preventDefault();
        modalConfirm();
        return;
      }
      if (e.key === 'Backspace' && A().modal?.back) {
        e.preventDefault();
        const back = A().modal.back;
        A().closeModal(false);
        setTimeout(back, 0);
        return;
      }
      if (e.key === 'Delete' && kind === 'sessions') {
        e.preventDefault(); modalDeleteSession(); return;
      }
      return; // swallow other keys while modal open
    }

    // ── Global shortcuts ────────────────────────────────────
    if (e.ctrlKey && e.key === 'c') { e.preventDefault(); if (A().isStreaming) interrupt(); return; }
    if (e.key === 'F2')             { e.preventDefault(); toggleF2();         return; }
    if (e.key === 'Tab' && e.shiftKey) { e.preventDefault(); toggleAutoAccept(); return; }
    if (e.key === 'PageUp')         { e.preventDefault(); scrollPrompt(-1);   return; }
    if (e.key === 'PageDown')       { e.preventDefault(); scrollPrompt(+1);   return; }

    // Re-focus input on any printable key
    if (!e.ctrlKey && !e.altKey && !e.metaKey && e.key.length === 1) {
      const inp = document.getElementById('chat-input');
      if (document.activeElement !== inp) inp?.focus();
    }
  });
}

function toggleF2() {
  A().verboseTools = !A().verboseTools;
  if (A().settings) { A().settings.UI.VerboseTools = A().verboseTools; saveSettings(); }
}
function toggleAutoAccept() {
  A().autoAccept = !A().autoAccept;
  if (A().settings?.Agent) {
    A().settings.Agent.PermissionMode = A().autoAccept ? 'full-auto' : 'normal';
    saveSettings();
  }
}

// ══════════════════════════════════════════════════════════════════
//  Template-facing API — see the module doc comment
// ══════════════════════════════════════════════════════════════════
Object.assign(window, {
  // Modal + palette
  modalClose, modalConfirm, modalHover, selectPaletteItem,
  // HITL + form submits
  doConfirm, submitKey, submitAgentDetail,
  // File-tree sidebar
  toggleFiletree, ftToggle,
  // Formatting used from x-text / x-html
  fmtMs,
  renderMd:      renderMarkdown,
  summarizeArgs: formatToolArgs,
  summarizeResult: (name, result, args) =>
    formatToolResult(name, args, result, A().verboseTools, effectiveToolPreviewLines()),
});

// ══════════════════════════════════════════════════════════════════
//  Init  (runs after Alpine.js has initialised, via DOMContentLoaded)
// ══════════════════════════════════════════════════════════════════
async function init() {
  document.getElementById('boot-art').textContent = buildBootArt();
  await Promise.all([loadThemes(), loadStatus(), loadSettings()]);
  wireInput();
  wireKeyboard();
  window.addEventListener('resize', checkNoticeFit);
  document.getElementById('chat-input')?.focus();
}

// DOMContentLoaded fires AFTER all deferred/module scripts (including
// Alpine.js) have run, so Alpine.store('app') is guaranteed to exist when
// init() executes.
document.addEventListener('DOMContentLoaded', init);

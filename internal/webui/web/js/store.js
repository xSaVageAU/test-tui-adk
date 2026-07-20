// store.js — the Alpine store: all reactive app state plus the helpers
// that mutate it (messages, palette, modal), the transient top-bar
// notice, and the loaders that hydrate state from the backend
// (status/settings). Importing this module registers the store via an
// alpine:init listener, so it must be evaluated before the Alpine CDN
// script runs — main.js's module tag precedes Alpine's deferred tag in
// index.html, which guarantees that.
//
// The theme.js/anim.js imports form module cycles (they import A back
// from here). That is safe: the cycle members only export function
// declarations, and every cross-module call happens at runtime, long
// after all modules have evaluated.

import { scrollBottom, shortId, humanK } from './format.js';
import { applyTheme } from './theme.js';
import { setLoader, stopSpinning } from './anim.js';
import { COMMANDS } from './commands.js';
import * as api from './api.js';

// ══════════════════════════════════════════════════════════════════
//  Alpine.js store  (registered before Alpine initialises)
// ══════════════════════════════════════════════════════════════════
document.addEventListener('alpine:init', () => {
  Alpine.store('app', {
    // ── Session / header ─────────────────────────────────────
    sessionTag:    'sess_…',
    sessionId:     '',
    contextBar:    '',
    contextWindow: 0,
    targetDescription: 'local host',
    lastBackendNote: '',
    notice:        '',   // transient top-bar status badge; see showNotice()

    // ── File-tree sidebar ────────────────────────────────────
    filetreeOpen: false,
    fileTreeRoot: '',
    fileTreeRows: [],   // visible rows only: { path, name, dir, depth, expanded, note }

    // ── Messages ─────────────────────────────────────────────
    messages:       [],
    msgCounter:     0,
    agentMsgIdx:    -1,   // index of the currently-building agent bubble; -1 = none
    toolMap:        {},   // toolCallId → messages[] index
    userMsgIds:     [],   // msg.id values for user messages (pgup/pgdn)
    promptScrollPos: -1,

    // ── Streaming UI ─────────────────────────────────────────
    isStreaming:  false,
    spinner:      '',
    workingLabel: 'thinking',

    // ── Settings toggles ─────────────────────────────────────
    verboseTools:  false,
    autoAccept:    false,
    showReasoning: true,
    highlightUser: false,
    settings:      null,

    // ── Themes ───────────────────────────────────────────────
    themes:       [],
    currentTheme: '',
    agentsData:   null,

    // ── Modal ─────────────────────────────────────────────────
    modalVisible: false,
    modal:        null,  // { kind, title, items, idx, data?, back? }

    // ── Command palette ───────────────────────────────────────
    paletteVisible: false,
    paletteMatches: [],
    paletteIdx:     0,

    // ──────────────────────────────────────────────────────────
    //  Message helpers
    // ──────────────────────────────────────────────────────────
    newId() { return ++this.msgCounter; },

    /** Push any message object into the list. Assigns .id and scrolls. */
    _push(m) {
      m.id = this.newId();
      this.messages.push(m);
      Alpine.nextTick(scrollBottom);
      return m;
    },

    pushUserMsg(text) {
      const m = this._push({ type: 'user', text });
      this.userMsgIds.push(m.id);
    },

    sysMsg(text) {
      this._push({ type: 'system', text });
    },

    /**
     * Lazily open a new agent bubble — only called when the first
     * text or reasoning chunk actually arrives. This prevents the
     * "empty agent bubble gets reused" bug from the old approach.
     */
    ensureAgentBubble() {
      if (this.agentMsgIdx >= 0) return; // already open
      this.agentMsgIdx = this.messages.length;
      this._push({
        type: 'agent', text: '', reasoning: '',
        reasoningDone: false, reasoningMs: 0, collapsed: true,
      });
    },

    appendText(delta) {
      this.ensureAgentBubble();
      this.messages[this.agentMsgIdx].text += delta;
      Alpine.nextTick(scrollBottom);
    },

    appendReasoning(delta, ms) {
      this.ensureAgentBubble();
      const m = this.messages[this.agentMsgIdx];
      m.reasoning += delta;
      if (ms !== undefined) { m.reasoningDone = true; m.reasoningMs = ms; }
    },

    /**
     * Close the current agent bubble.  Always called at stream end —
     * this is the fix for messages appending to the previous response.
     */
    closeAgentBubble() {
      this.agentMsgIdx = -1;
    },

    pushToolCall(id, name, args) {
      // Drop any open empty agent bubble so it doesn't show a blank line
      if (this.agentMsgIdx >= 0) {
        const m = this.messages[this.agentMsgIdx];
        if (!m.text && !m.reasoning) {
          this.messages.splice(this.agentMsgIdx, 1);
          // Adjust toolMap indices that shifted due to splice
          for (const k of Object.keys(this.toolMap)) {
            if (this.toolMap[k] > this.agentMsgIdx) this.toolMap[k]--;
          }
        }
        this.agentMsgIdx = -1;
      }
      const idx = this.messages.length;
      this._push({ type: 'tool', id, name, args, status: 'running', result: null, confirmId: null, hint: '' });
      this.toolMap[id] = idx;
    },

    updateToolResult(id, result) {
      const idx = this.toolMap[id];
      if (idx == null) return;
      this.messages[idx].result = result;
      this.messages[idx].status = 'done';
    },

    pendingToolConfirm(id, confirmId, hint) {
      const idx = this.toolMap[id];
      if (idx == null) return;
      this.messages[idx].status    = 'pending';
      this.messages[idx].confirmId = confirmId;
      this.messages[idx].hint      = hint || 'Approve to continue';
      Alpine.nextTick(scrollBottom);
    },

    resolveToolConfirm(id, approved) {
      const idx = this.toolMap[id];
      if (idx == null) return;
      this.messages[idx].status    = approved ? 'approved' : 'denied';
      this.messages[idx].confirmId = null;
    },

    clearMessages() {
      this.messages        = [];
      this.agentMsgIdx     = -1;
      this.toolMap         = {};
      this.userMsgIds      = [];
      this.promptScrollPos = -1;
    },

    // ──────────────────────────────────────────────────────────
    //  Command palette
    // ──────────────────────────────────────────────────────────
    updatePalette(val) {
      if (!val.startsWith('/')) { this.closePalette(); return; }
      const q = val.slice(1).toLowerCase();
      const matches = COMMANDS.filter(c => c.name.slice(1).startsWith(q));
      if (!matches.length) { this.closePalette(); return; }
      this.paletteMatches = matches;
      if (this.paletteIdx >= matches.length) this.paletteIdx = 0;
      this.paletteVisible = true;
    },

    closePalette() {
      this.paletteVisible = false;
      this.paletteMatches = [];
      this.paletteIdx     = 0;
    },

    paletteMove(d) {
      const n = this.paletteMatches.length;
      if (!n) return;
      this.paletteIdx = (this.paletteIdx + d + n) % n;
    },

    // ──────────────────────────────────────────────────────────
    //  Modal
    // ──────────────────────────────────────────────────────────
    openModal(opts) {
      this.modal        = { idx: 0, items: [], ...opts };
      this.modalVisible = true;
    },

    closeModal(revertAll) {
      if (revertAll) {
        if (this.modal?.kind === 'theme' && this.modal.data?.origin) {
          applyTheme(this.modal.data.origin, false);
        }
        if (this.modal?.kind === 'loader' && this.modal.data?.origin) {
          setLoader(this.modal.data.origin, false);
        }
      }
      this.modalVisible = false;
      this.modal        = null;
      // /loader's live preview drives the spin timer on its own; once the
      // picker closes, only an in-flight turn justifies keeping it running.
      if (!this.isStreaming) stopSpinning();
      document.getElementById('chat-input')?.focus();
    },

    modalMove(d) {
      if (!this.modal?.items?.length) return;
      const n = this.modal.items.length;
      this.modal.idx = (this.modal.idx + d + n) % n;
      // Live theme/loader preview
      if (this.modal.kind === 'theme') {
        applyTheme(this.modal.items[this.modal.idx].id, false);
      }
      if (this.modal.kind === 'loader') {
        setLoader(this.modal.items[this.modal.idx].id, false);
      }
      Alpine.nextTick(() => {
        document.querySelector('#modal-body .modal-row.selected')
          ?.scrollIntoView({ block: 'nearest' });
      });
    },
  });
});

// ══════════════════════════════════════════════════════════════════
//  Store shortcut
// ══════════════════════════════════════════════════════════════════
export function A() { return Alpine.store('app'); }

// ══════════════════════════════════════════════════════════════════
//  Top-bar status notice — mirrors the TUI's notice.go/setNotice split:
//  confirmations and progress notes show briefly in the center of the
//  top bar and expire; failures and refusals stay chat sysMsg badges,
//  since an error that vanishes after 4 seconds would be data loss.
// ══════════════════════════════════════════════════════════════════
const NOTICE_DURATION = 4000; // TUI noticeDuration

let noticeTimer = null;

export function showNotice(text) {
  A().notice = text;
  if (noticeTimer) clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => { A().notice = ''; noticeTimer = null; }, NOTICE_DURATION);
  Alpine.nextTick(checkNoticeFit);
}

// Hidden — never pushed aside, never truncated — when the centered badge
// would collide with the session tag or context bar (TUI parity:
// header.go's joinLeftCenterRight drops the notice the same way).
export function checkNoticeFit() {
  const el = document.getElementById('status-notice');
  if (!el || !A().notice) return;
  el.style.visibility = 'visible';
  const n = el.getBoundingClientRect();
  const l = document.getElementById('session-tag')?.getBoundingClientRect();
  const r = document.getElementById('context-bar')?.getBoundingClientRect();
  if ((l && n.left < l.right + 8) || (r && r.width > 0 && n.right > r.left - 8)) {
    el.style.visibility = 'hidden';
  }
}

// ══════════════════════════════════════════════════════════════════
//  Context bar
// ══════════════════════════════════════════════════════════════════
export function updateContextBar(used, win) {
  if (!win) { A().contextBar = ''; return; }
  const f = Math.round(Math.min(used / win, 1) * 10);
  A().contextBar = humanK(used) + '/' + humanK(win) + ' ' + '█'.repeat(f) + '░'.repeat(10 - f);
}

// ══════════════════════════════════════════════════════════════════
//  Backend state hydration
// ══════════════════════════════════════════════════════════════════
export async function loadStatus() {
  try {
    const d = await api.getStatus();
    A().sessionId     = d.activeSessionId || A().sessionId;
    A().sessionTag    = shortId(A().sessionId);
    A().contextWindow = d.contextWindow || 0;
    A().targetDescription = d.targetDescription || 'local host';
    updateContextBar(0, A().contextWindow);
    if (d.backendNote && d.backendNote !== A().lastBackendNote) {
      A().sysMsg(d.backendNote);
      A().lastBackendNote = d.backendNote;
    }
  } catch (e) { console.error('loadStatus', e); }
}

export async function loadSettings() {
  try {
    const d = await api.getSettings();
    A().settings      = d;
    A().verboseTools   = !!d.UI?.VerboseTools;
    A().autoAccept     = d.Agent?.PermissionMode === 'full-auto';
    A().showReasoning  = !d.UI?.HideReasoningText;
    A().highlightUser  = !!d.UI?.HighlightUser;
  } catch (e) { console.error('loadSettings', e); }
}

export async function saveSettings() {
  if (!A().settings) return;
  try {
    await api.putSettings(A().settings);
  } catch (e) { console.error('saveSettings', e); }
}

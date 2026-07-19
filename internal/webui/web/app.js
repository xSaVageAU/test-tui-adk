'use strict';

// ══════════════════════════════════════════════════════════════════
//  Boot art glyph map — mirrors internal/ui/bootart.go
// ══════════════════════════════════════════════════════════════════
const BOOT_GLYPHS = {
  A: ['.###.','#...#','#####','#...#','#...#'],
  G: ['.###.','#....','#.###','#...#','.###.'],
  E: ['#####','#....','###..','#....','#####'],
  N: ['#...#','##..#','#.#.#','#..##','#...#'],
  T: ['#####','..#..','..#..','..#..','..#..'],
};

function buildBootArt() {
  const rows = Array(5).fill('');
  for (const ch of 'AGENT') {
    const g = BOOT_GLYPHS[ch]; if (!g) continue;
    for (let r = 0; r < 5; r++) {
      for (const c of g[r]) rows[r] += c === '#' ? '██' : '\u00a0\u00a0';
      rows[r] += '\u00a0\u00a0'; // letter gap
    }
  }
  return rows.join('\n');
}

// ══════════════════════════════════════════════════════════════════
//  Command registry — mirrors commandSpecs in internal/ui/commands.go
// ══════════════════════════════════════════════════════════════════
const COMMANDS = [
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
function A() { return Alpine.store('app'); }

// ══════════════════════════════════════════════════════════════════
//  Scroll
// ══════════════════════════════════════════════════════════════════
function scrollBottom() {
  const el = document.getElementById('chat-area');
  if (el) el.scrollTop = el.scrollHeight;
}

// ══════════════════════════════════════════════════════════════════
//  Working Animations (Ported from internal/ui/workinganim_variants.go)
// ══════════════════════════════════════════════════════════════════
function lerpColor(hexA, hexB, amount) {
  if (!hexA || !hexB) return '#ffffff';
  const cleanA = hexA.replace('#', '');
  const cleanB = hexB.replace('#', '');
  const rA = parseInt(cleanA.slice(0, 2), 16);
  const gA = parseInt(cleanA.slice(2, 4), 16);
  const bA = parseInt(cleanA.slice(4, 6), 16);

  const rB = parseInt(cleanB.slice(0, 2), 16);
  const gB = parseInt(cleanB.slice(2, 4), 16);
  const bB = parseInt(cleanB.slice(4, 6), 16);

  const r = Math.round(rA + (rB - rA) * amount);
  const g = Math.round(gA + (gB - gA) * amount);
  const b = Math.round(bA + (bB - bA) * amount);

  return '#' + [r, g, b].map(x => x.toString(16).padStart(2, '0')).join('');
}

// scaleColor darkens a hex color by multiplying each channel by f —
// mirrors the TUI Orbit trail's fade-toward-black (workinganim_variants.go),
// which is *not* the same as lerping toward the theme background.
function scaleColor(hex, f) {
  const c = (hex || '#000000').replace('#', '');
  const ch = i => Math.round(parseInt(c.slice(i, i + 2), 16) * f)
    .toString(16).padStart(2, '0');
  return '#' + ch(0) + ch(2) + ch(4);
}

// escapeHtml — animation output is injected via x-html, so any rune that
// is HTML-significant (Matrix Rain's <>[]{} tail) must be escaped or it
// silently corrupts the markup.
function escapeHtml(ch) {
  return ch === '&' ? '&amp;' : ch === '<' ? '&lt;' : ch === '>' ? '&gt;' : ch;
}

function getThemeColors() {
  if (!A()) return {};
  const current = A().currentTheme;
  const t = A().themes?.find(x => x.name === current) || A().themes?.[0] || {};
  return {
    accent: t.accent || '#00d7d7',
    accentMuted: t.accentMuted || '#005f5f',
    warning: t.warning || '#d7af00',
    error: t.error || '#d70000',
    success: t.success || '#00af5f',
    text: t.text || '#e8e8e8',
    textMuted: t.textMuted || '#777777',
    textFaint: t.textFaint || '#444444',
    surface: t.surface || '#1a1a1a',
    highlight: t.highlight || '#2a2a2a',
    background: t.background || '#000000'
  };
}

// Measured character width (canvas measureText on the pre's actual font)
// instead of the old hardcoded /8 guess — the guess drifted with zoom and
// font fallback, leaving the animations either clipped or short of the
// full row width the TUI always fills.
let _charWidth = { font: '', w: 0 };
function animCharWidth(pre) {
  const cs = getComputedStyle(pre);
  const font = `${cs.fontStyle} ${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`;
  if (_charWidth.font !== font) {
    const canvas = animCharWidth._c || (animCharWidth._c = document.createElement('canvas'));
    const ctx = canvas.getContext('2d');
    ctx.font = font;
    _charWidth = { font, w: ctx.measureText('0'.repeat(100)).width / 100 };
  }
  return _charWidth.w;
}

function getAnimWidth() {
  const pre = document.querySelector('#working-row pre');
  if (!pre) return 80;
  const cw = animCharWidth(pre);
  if (!cw) return 80;
  return Math.max(20, Math.floor(pre.clientWidth / cw));
}

const GLITCH_RUNES = "▓█░▒╔╗╚╝║═╠╣╦╩╬⌂◙◘☺☻♦♣♠♥";
const STATUS_MESSAGES = [
  "Thinking deeply about your request and context…",
  "Reasoning through the problem space carefully… ",
  "Generating a thoughtful and nuanced response…  ",
  "Consulting knowledge and crafting an answer…   "
];
// Same rune set as the TUI's randomRainRune — the <>[]{} tail is safe here
// because every rain rune goes through escapeHtml before injection.
const RAIN_RUNES = "ｦｧｨｩｪｫｬｭｮｯｰｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜﾝ0123456789<>[]{}+-*";
const BRAILLE_RUNES = "⠁⠂⠄⡀⢀⠠⠐⠈⠃⠅⠘⠨⠰⣀⡄⢠⠸⡇⢸⣆⣇⣷⣾⣿";

function renderAnim(name, t, width, frame, label) {
  switch (name) {
    case "Equalizer": {
      const runes = "▁▂▃▄▅▆▇█";
      let html = "";
      const phase = frame * 0.2;
      for (let i = 0; i < width; i++) {
        const v = (Math.sin(i * 0.15 + phase) + Math.sin(i * 0.37 - phase * 0.7) + 2.0) / 4.0;
        const idx = Math.floor(v * (runes.length - 1));
        const col = lerpColor(t.accent, t.warning, i / width);
        html += `<span style="color:${col}">${runes[idx]}</span>`;
      }
      return html;
    }

    case "Pulse Wave": {
      const frameVal = frame * 0.05;
      const chars = "▁▂▃▄▅▆▇█▇▆▅▄▃▂▁";
      let html = "";
      for (let i = 0; i < width; i++) {
        const phase = (i / width) * Math.PI * 6 - frameVal * 2;
        const v = (Math.sin(phase) + 1) / 2;
        const idx = Math.floor(v * (chars.length - 1));
        let col = lerpColor(t.accent, t.warning, v);
        if (v > 0.85) {
          col = lerpColor(t.warning, t.text, (v - 0.85) / 0.15);
        }
        html += `<span style="color:${col}">${chars[idx]}</span>`;
      }
      return html;
    }

    case "Orbit": {
      const frameVal = frame * 0.07;
      const cx = width / 2;
      let radius = width / 2 - 4;
      if (radius < 5) radius = 5;
      const cells = Array.from({ length: width }, () => ({ ch: ' ', col: t.text }));
      const dots = [
        { angle: frameVal, colA: t.accent, colB: t.accentMuted },
        { angle: frameVal + Math.PI, colA: t.warning, colB: t.error }
      ];
      const trailLen = 12;
      for (const dot of dots) {
        for (let ti = 0; ti < trailLen; ti++) {
          const a = dot.angle - ti * 0.18;
          const x = Math.floor(cx + Math.cos(a) * radius);
          if (x < 0 || x >= width) continue;
          const fade = 1.0 - ti / trailLen;
          let col = lerpColor(dot.colB, dot.colA, fade);
          if (ti > 0) {
            col = scaleColor(col, fade); // TUI fades the trail toward black
          }
          cells[x].ch = ti === 0 ? '●' : '·';
          cells[x].col = col;
        }
      }
      const labelStr = " " + label + " ";
      const lStart = Math.floor(width / 2 - labelStr.length / 2);
      for (let i = 0; i < labelStr.length; i++) {
        const pos = lStart + i;
        if (pos >= 0 && pos < width) {
          cells[pos].ch = labelStr[i];
          cells[pos].col = t.text;
        }
      }
      let html1 = "";
      for (const c of cells) {
        html1 += `<span style="color:${c.col}">${c.ch}</span>`;
      }
      const pulse = frame * 0.08;
      let html2 = "";
      for (let i = 0; i < width; i++) {
        const v = (Math.sin((i / width) * Math.PI * 4 - pulse) + 1) / 2 * 0.5;
        const col = lerpColor(t.surface, t.accentMuted, v);
        html2 += `<span style="color:${col}">▄</span>`;
      }
      return html1 + "\n" + html2;
    }

    case "Glitch Scan": {
      const msg = STATUS_MESSAGES[Math.floor(frame / 80) % STATUS_MESSAGES.length];
      const msgLen = Math.min(msg.length, width);
      const scanPos = Math.floor((frame * 2) % (width + 20) - 10);
      const scanWidth = 8;
      let html1 = "";
      for (let i = 0; i < width; i++) {
        const dist = i - scanPos;
        const inScan = dist >= 0 && dist < scanWidth && i < msgLen;
        let ch = ' ';
        let col = t.text;
        let bold = false;
        if (inScan) {
          const fade = 1.0 - dist / scanWidth;
          if (Math.random() < fade * 0.8) {
            ch = GLITCH_RUNES[Math.floor(Math.random() * GLITCH_RUNES.length)];
          } else {
            ch = msg[i];
          }
          col = lerpColor(t.warning, t.text, fade);
          bold = true;
        } else if (i < msgLen) {
          ch = msg[i];
          col = i < scanPos ? t.accentMuted : t.highlight;
        }
        const fw = bold ? "font-weight:bold" : "";
        html1 += `<span style="color:${col};${fw}">${ch}</span>`;
      }
      const progress = Math.max(0, Math.min(1, (scanPos + 10) / (width + 20)));
      const filled = Math.floor(progress * width);
      let html2 = "";
      for (let i = 0; i < width; i++) {
        if (i < filled) {
          const col = lerpColor(t.accentMuted, t.accent, i / width);
          html2 += `<span style="color:${col}">▬</span>`;
        } else {
          html2 += `<span style="color:${t.highlight}">─</span>`;
        }
      }
      return html1 + "\n" + html2;
    }

    case "Cylon Scanner": {
      const period = 2 * (width - 1);
      let pos = frame % period;
      if (pos >= width) pos = period - pos;
      let html = "";
      for (let i = 0; i < width; i++) {
        const dist = Math.abs(i - pos);
        if (dist < 6) {
          const fade = 1.0 - dist / 6.0;
          const col = lerpColor(t.surface, t.accent, fade);
          let ch = "░";
          if (dist === 0) ch = "█";
          else if (dist < 2) ch = "▓";
          else if (dist < 4) ch = "▒";
          html += `<span style="color:${col}">${ch}</span>`;
        } else {
          html += `<span style="color:${t.highlight}">─</span>`;
        }
      }
      return html;
    }

    case "Bouncing Dots": {
      const dotsCount = 7;
      const spacing = 4;
      const start = Math.floor(width / 2 - (dotsCount * spacing) / 2);
      const cells = Array.from({ length: width }, () => ({ ch: ' ', col: t.surface }));
      for (let j = 0; j < dotsCount; j++) {
        const pos = start + j * spacing;
        if (pos < 0 || pos >= width) continue;
        const val = (Math.sin(frame * 0.25 - j * 0.7) + 1.0) / 2.0;
        let ch = " ";
        let col = t.surface;
        if (val > 0.8) {
          ch = "●"; col = t.accent;
        } else if (val > 0.5) {
          ch = "•"; col = t.accentMuted;
        } else if (val > 0.25) {
          ch = "·"; col = t.textFaint;
        }
        cells[pos].ch = ch;
        cells[pos].col = col;
      }
      let html1 = "";
      for (const c of cells) {
        html1 += `<span style="color:${c.col}">${c.ch}</span>`;
      }
      // TUI renders this label at the line start, not centered. anim-label
      // exempts this multi-char span from the CSS 1ch cell lock.
      const html2 = `<span class="anim-label" style="color:${t.textMuted}"> aligning thoughts </span>`;
      return html1 + "\n" + html2;
    }

    case "Matrix Rain": {
      const line1 = [];
      const line2 = [];
      for (let c = 0; c < width; c++) {
        const speed = 0.15 + 0.05 * Math.sin(c * 3.7);
        const offset = Math.sin(c * 1.1) * 10.0;
        const y = (frame * speed + offset) % 8.0;
        for (let r = 0; r < 2; r++) {
          const dist = y - r;
          let ch = ' ';
          let col = t.surface;
          let bold = false;
          if (dist >= 0 && dist < 3.0) {
            const fade = 1.0 - (dist / 3.0);
            ch = RAIN_RUNES[Math.floor(Math.random() * RAIN_RUNES.length)];
            if (dist < 0.5) {
              col = t.text;
              bold = true;
            } else {
              col = lerpColor(t.surface, t.success, fade);
            }
          }
          const fw = bold ? "font-weight:bold" : "";
          const s = `<span style="color:${col};${fw}">${escapeHtml(ch)}</span>`;
          if (r === 0) line1.push(s);
          else line2.push(s);
        }
      }
      return line1.join('') + "\n" + line2.join('');
    }

    case "Braille Wave": {
      const frameVal = frame * 0.08;
      let html1 = "";
      let html2 = "";
      for (let i = 0; i < width; i++) {
        const phase = (i / width) * Math.PI * 4 - frameVal;
        const v1 = (Math.sin(phase) + 1) / 2;
        const v2 = (Math.sin(phase * 1.3 + Math.PI / 3) + 1) / 2;
        const idx1 = Math.floor(v1 * (BRAILLE_RUNES.length - 1));
        const idx2 = Math.floor(v2 * (BRAILLE_RUNES.length - 1));
        html1 += `<span style="color:${lerpColor(t.surface, t.accent, v1)}">${BRAILLE_RUNES[idx1]}</span>`;
        html2 += `<span style="color:${lerpColor(t.surface, t.accentMuted, v2)}">${BRAILLE_RUNES[idx2]}</span>`;
      }
      return html1 + "\n" + html2;
    }

    case "Radar Sweep": {
      const pos = frame % width;
      const trailLen = 10;
      let html = "";
      for (let i = 0; i < width; i++) {
        let d = pos - i;
        if (d < 0) d += width;
        if (d === 0) {
          html += `<span style="color:${t.text};font-weight:bold">▐</span>`;
        } else if (d < trailLen) {
          const fade = 1.0 - d / trailLen;
          html += `<span style="color:${lerpColor(t.surface, t.accent, fade)}">·</span>`;
        } else {
          html += `<span style="color:${t.highlight}">─</span>`;
        }
      }
      return html;
    }

    case "Slash Trail": {
      const offset = Math.floor(frame / 2) % 6;
      let html1 = "";
      let html2 = "";
      for (let i = 0; i < width; i++) {
        const col = lerpColor(t.accentMuted, t.accent, (Math.sin(i * 0.3 + frame * 0.05) + 1) / 2);
        if ((i + offset) % 6 === 0) {
          html1 += `<span style="color:${col}">/</span>`;
        } else {
          html1 += " ";
        }
        if ((i + 1 + offset) % 6 === 0) {
          html2 += `<span style="color:${col}">/</span>`;
        } else {
          html2 += " ";
        }
      }
      return html1 + "\n" + html2;
    }

    default:
      return "thinking…";
  }
}

let spinTimer = null;
// Persistent across start/stop like the TUI's App-lifetime frame counter —
// restarting mid-turn (HITL resume) must not visibly snap every animation
// back to its frame-0 pose.
let animFrame = 0;

// The TUI reserves exactly 2 rows and pads short (1-line) variants with a
// blank line on *top*, so single-line animations anchor to the bottom,
// right against the input box (workinganim.go render()). Mirror that here.
const ANIM_HEIGHT = 2;
function padAnimLines(out) {
  const lines = out.split("\n");
  while (lines.length < ANIM_HEIGHT) lines.unshift("");
  return lines.slice(-ANIM_HEIGHT).join("\n");
}

function renderSpinnerFrame() {
  const name = A().settings?.UI?.WorkingAnim || "Equalizer";
  const t = getThemeColors();
  const w = getAnimWidth();
  A().spinner = padAnimLines(renderAnim(name, t, w, animFrame, A().workingLabel));
}

function startSpinning() {
  stopSpinning();
  // rAF with a fixed 60ms accumulator instead of setTimeout(60): timer
  // callbacks drift/stutter in browsers, which made the smooth waves
  // (Pulse Wave etc.) visibly hitch. This paces frames evenly against
  // the display clock at the TUI's tea.Tick(60ms) rate.
  const tickRate = 60;
  let last = performance.now();
  let acc = tickRate; // render immediately on the first rAF
  const step = now => {
    spinTimer = requestAnimationFrame(step);
    acc += now - last;
    last = now;
    if (acc < tickRate) return;
    // Advance whole ticks, but never spiral to catch up after a
    // background-tab stall.
    animFrame += Math.min(4, Math.floor(acc / tickRate));
    acc %= tickRate;
    renderSpinnerFrame();
  };
  spinTimer = requestAnimationFrame(step);
}

function stopSpinning() {
  if (spinTimer) { cancelAnimationFrame(spinTimer); spinTimer = null; }
  // Blank the reserved rows (TUI blankWorkingAnim) rather than freezing
  // on the last frame.
  if (window.Alpine && A()) A().spinner = '';
}

function setStreaming(on, label) {
  A().isStreaming = on;
  if (on) {
    if (label) A().workingLabel = label;
    startSpinning();
  } else {
    stopSpinning();
  }
}

function setWorkingLabel(l) { A().workingLabel = l; }

// ══════════════════════════════════════════════════════════════════
//  Theme
// ══════════════════════════════════════════════════════════════════
function applyTheme(name, announce) {
  const t = A().themes.find(x => x.name === name);
  if (!t) return;
  A().currentTheme = name;
  localStorage.setItem('webui-theme', name);
  const s = document.documentElement.style;
  s.setProperty('--bg',           t.background);
  s.setProperty('--surface',      t.surface);
  s.setProperty('--highlight',    t.highlight);
  s.setProperty('--border',       t.border);
  s.setProperty('--border-focus', t.borderFocus);
  s.setProperty('--text',         t.text);
  s.setProperty('--text-muted',   t.textMuted);
  s.setProperty('--text-faint',   t.textFaint);
  s.setProperty('--text-on-fill', t.textOnFill);
  s.setProperty('--accent',       t.accent);
  s.setProperty('--accent-muted', t.accentMuted);
  s.setProperty('--reasoning',    t.reasoning);
  s.setProperty('--success',      t.success);
  s.setProperty('--warning',      t.warning);
  s.setProperty('--error',        t.error);
  s.setProperty('--attention',    t.attention);
  if (announce) A().sysMsg('Theme set to ' + name + '.');
}

// ══════════════════════════════════════════════════════════════════
//  Context bar
// ══════════════════════════════════════════════════════════════════
function updateContextBar(used, win) {
  if (!win) { A().contextBar = ''; return; }
  const f = Math.round(Math.min(used / win, 1) * 10);
  A().contextBar = humanK(used) + '/' + humanK(win) + ' ' + '█'.repeat(f) + '░'.repeat(10 - f);
}
function humanK(n) { return n >= 1e6 ? (n/1e6).toFixed(1)+'M' : n >= 1e3 ? Math.round(n/1e3)+'k' : String(n); }
function shortId(id) {
  const s = (id || '').replace(/-/g,'');
  return 'sess_' + (s.length > 8 ? s.slice(0,8) : s || '????????');
}

// ══════════════════════════════════════════════════════════════════
//  Data loading
// ══════════════════════════════════════════════════════════════════
async function loadThemes() {
  try {
    const list = await fetch('/api/themes').then(r => r.json());
    A().themes = list;
    applyTheme(localStorage.getItem('webui-theme') || list[0]?.name || '', false);
  } catch (e) { console.error('loadThemes', e); }
}

async function loadStatus() {
  try {
    const d = await fetch('/api/status').then(r => r.json());
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

async function loadSettings() {
  try {
    const d = await fetch('/api/settings').then(r => r.json());
    A().settings      = d;
    A().verboseTools   = !!d.UI?.VerboseTools;
    A().autoAccept     = d.Agent?.PermissionMode === 'full-auto';
    A().showReasoning  = !d.UI?.HideReasoningText;
    A().highlightUser  = !!d.UI?.HighlightUser;
  } catch (e) { console.error('loadSettings', e); }
}

async function saveSettings() {
  if (!A().settings) return;
  try {
    await fetch('/api/settings', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(A().settings),
    });
  } catch (e) { console.error('saveSettings', e); }
}

// ══════════════════════════════════════════════════════════════════
//  Streaming
// ══════════════════════════════════════════════════════════════════
let evtSrc = null;

function sendMessage(text) {
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
function finishStream(keepStreaming) {
  if (evtSrc) { evtSrc.close(); evtSrc = null; }
  A().closeAgentBubble(); // ← always reset; prevents next response re-using old bubble
  if (!keepStreaming) setStreaming(false);
  document.getElementById('chat-input')?.focus();
}

async function interrupt() {
  try { await fetch('/api/interrupt', { method: 'POST' }); } catch {}
  finishStream();
  A().sysMsg('Interrupted.');
}

// ══════════════════════════════════════════════════════════════════
//  HITL confirmation (called from Alpine template via window.doConfirm)
// ══════════════════════════════════════════════════════════════════
async function doConfirmImpl(confirmId, origId, approved) {
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

// ══════════════════════════════════════════════════════════════════
//  Commands
// ══════════════════════════════════════════════════════════════════
function runCommand(name) {
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
    const d = await fetch('/api/sessions', { method: 'POST' }).then(r => r.json());
    A().sessionId  = d.sessionId;
    A().sessionTag = shortId(d.sessionId);
    A().clearMessages();
    A().sysMsg('New session started.');
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

async function cmdSessions() {
  try {
    const list = await fetch('/api/sessions').then(r => r.json());
    if (!list?.length) { A().sysMsg('No past sessions found.'); return; }
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

const LOADER_NAMES = [
  "Equalizer",
  "Pulse Wave",
  "Orbit",
  "Glitch Scan",
  "Cylon Scanner",
  "Bouncing Dots",
  "Matrix Rain",
  "Braille Wave",
  "Radar Sweep",
  "Slash Trail"
];

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

// announce doubles as "commit": previews (arrow-key moves, escape-revert)
// only touch the in-memory setting the spin loop reads; the TUI likewise
// persists workingAnim only when a choice is confirmed.
function setLoader(name, announce) {
  if (!A().settings) return;
  A().settings.UI.WorkingAnim = name;
  if (announce) {
    saveSettings();
    A().sysMsg('Working animation set to ' + name + '.');
  }
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
    const res = await fetch('/api/agents');
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
  A().sysMsg('Reloading agents…');
  try {
    const res = await fetch('/api/agents', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: '', provider: '', model: '', tools: [] }),
    });
    A().sysMsg(res.ok ? 'Agents reloaded.' : 'Reload failed: ' + res.statusText);
    if (res.ok) await loadStatus();
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

// ══════════════════════════════════════════════════════════════════
//  Modal confirmation logic
// ══════════════════════════════════════════════════════════════════

/**
 * Confirm the currently-selected (or a clicked) modal item.
 * i is optional — if supplied, modal.idx is updated first.
 */
function modalConfirmAtIdx(i) {
  if (!A().modal) return;
  if (i !== undefined) A().modal.idx = i;
  const item = A().modal.items[A().modal.idx];
  if (!item && A().modal.kind !== 'key' && A().modal.kind !== 'agent-detail') return;
  _doModalConfirm(item);
}

function _doModalConfirm(item) {
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
  await loadStatus();
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
  A().sysMsg('Loading session ' + shortId(id) + '…');
  try {
    const entries = await fetch('/api/transcript/' + id).then(r => r.json());
    // Remove the "Loading…" badge
    if (A().messages.at(-1)?.type === 'system') A().messages.splice(-1, 1);
    replayTranscript(entries);
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

async function submitKeyImpl() {
  const provider = document.getElementById('mf-provider')?.value || 'gemini';
  const key      = document.getElementById('mf-key')?.value?.trim() || '';
  if (!key) { A().sysMsg('Key cannot be empty.'); return; }
  try {
    const res = await fetch('/api/key', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ provider, key }),
    });
    if (res.ok) {
      A().closeModal(false);
      A().sysMsg('API key saved for ' + provider + '.');
      await loadStatus();
    } else {
      const d = await res.json().catch(() => ({}));
      A().sysMsg('Key error: ' + (d.error || res.statusText));
    }
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

async function submitAgentDetailImpl() {
  const id       = A().modal?.data?.agentId;
  const provider = document.getElementById('mf-aprov')?.value  || '';
  const model    = document.getElementById('mf-amodel')?.value || '';
  const tools    = Array.from(document.querySelectorAll('#modal-body input[type=checkbox]:checked')).map(b => b.value);
  try {
    const res = await fetch('/api/agents', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, provider, model, tools }),
    });
    A().closeModal(false);
    A().sysMsg(res.ok ? 'Agent config saved. Backend rebuilding…' : 'Save failed: ' + res.statusText);
    if (res.ok) await loadStatus();
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

async function modalDeleteSession() {
  if (A().modal?.kind !== 'sessions') return;
  const item = A().modal.items[A().modal.idx];
  if (!item || !confirm('Delete session ' + item.name + '?')) return;
  try {
    await fetch('/api/sessions/' + item.id, { method: 'DELETE' });
    if (item.id === A().sessionId) await cmdNew();
    else await cmdSessions();
  } catch (e) { A().sysMsg('Error: ' + e.message); }
}

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
        modalConfirmAtIdx();
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
//  Functions exposed to Alpine templates via window
// ══════════════════════════════════════════════════════════════════
window.fmtMs             = ms => ms >= 1000 ? (ms/1000).toFixed(1)+'s' : ms+'ms';
window.doConfirm         = (cid, oid, approved) => doConfirmImpl(cid, oid, approved);
window.submitKey         = () => submitKeyImpl();
window.submitAgentDetail = () => submitAgentDetailImpl();

// Modal actions
window.modalClose   = ()    => A().closeModal(true);
window.modalConfirm = i     => modalConfirmAtIdx(i);
window.modalHover   = i     => {
  if (!A().modal || A().modal.idx === i) return;
  A().modal.idx = i;
  if (A().modal.kind === 'theme')  applyTheme(A().modal.items[i].id, false);
  if (A().modal.kind === 'loader') setLoader(A().modal.items[i].id, false);
};

// Palette
window.selectPaletteItem = i => {
  const cmd = A().paletteMatches[i]; if (!cmd) return;
  document.getElementById('chat-input').value = '';
  A().closePalette();
  setTimeout(() => runCommand(cmd.name), 0);
};

// Formatting (called from x-html / x-text)
window.renderMd       = text => renderMarkdown(text);
window.summarizeArgs  = (name, args) => {
  if (!args || !Object.keys(args).length) return '';
  if (args.path)    return String(args.path);
  if (args.command) return String(args.command).slice(0, 80);
  if (args.url)     return String(args.url);
  if (A().verboseTools) return JSON.stringify(args);
  const v = Object.values(args)[0];
  const s = typeof v === 'string' ? v : JSON.stringify(v);
  return s.length > 80 ? s.slice(0, 80) + '…' : s;
};
window.summarizeResult = (name, result) => {
  if (result == null) return '';
  if (A().verboseTools) return JSON.stringify(result, null, 2);
  const s = typeof result === 'string' ? result : JSON.stringify(result);
  return s.length > 120 ? s.slice(0, 120) + '…' : s;
};

// ══════════════════════════════════════════════════════════════════
//  Formatting helpers
// ══════════════════════════════════════════════════════════════════
function onOff(v)    { return v ? 'on' : 'off'; }
function fmtDate(iso){ try { return new Date(iso).toLocaleString(); } catch { return iso || ''; } }

function renderMarkdown(text) {
  if (!text) return '';
  let s = text
    .replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  // Code blocks
  s = s.replace(/```[\w]*\n([\s\S]*?)```/g, (_, c) => `<pre><code>${c.trim()}</code></pre>`);
  // Inline code
  s = s.replace(/`([^`\n]+)`/g, '<code>$1</code>');
  // Bold
  s = s.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>');
  // Headers
  s = s.replace(/^### (.+)$/gm, '<div class="md-h3">$1</div>');
  s = s.replace(/^## (.+)$/gm,  '<div class="md-h2">$1</div>');
  s = s.replace(/^# (.+)$/gm,   '<div class="md-h1">$1</div>');
  // Lists
  s = s.replace(/^[-*] (.+)$/gm, '<div class="md-li">$1</div>');
  // Paragraphs
  s = s.split(/\n{2,}/).map(b => {
    b = b.trim(); if (!b) return '';
    if (b.startsWith('<')) return b;
    return b.replace(/\n/g, '<br>');
  }).filter(Boolean).join('\n\n');
  return s;
}

// ══════════════════════════════════════════════════════════════════
//  Init  (runs after Alpine.js has initialised, via DOMContentLoaded)
// ══════════════════════════════════════════════════════════════════
async function init() {
  document.getElementById('boot-art').textContent = buildBootArt();
  await Promise.all([loadThemes(), loadStatus(), loadSettings()]);
  wireInput();
  wireKeyboard();
  document.getElementById('chat-input')?.focus();
}

// DOMContentLoaded fires AFTER all deferred scripts (including Alpine.js) have run,
// so Alpine.store('app') is guaranteed to exist when init() executes.
document.addEventListener('DOMContentLoaded', init);

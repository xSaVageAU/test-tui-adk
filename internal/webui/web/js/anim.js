// anim.js — the "working" animations (ported from
// internal/ui/workinganim_variants.go; keep changes in lockstep with the
// Go file) plus the spin-timer lifecycle that drives them. setLoader
// lives here too: previews only touch the in-memory setting the spin
// loop reads, and commit persists it — same split as the TUI.

import { A, showNotice, saveSettings } from './store.js';
import { getThemeColors } from './theme.js';

export const LOADER_NAMES = [
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

export function startSpinning() {
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

export function stopSpinning() {
  if (spinTimer) { cancelAnimationFrame(spinTimer); spinTimer = null; }
  // Blank the reserved rows (TUI blankWorkingAnim) rather than freezing
  // on the last frame.
  if (window.Alpine && A()) A().spinner = '';
}

export function setStreaming(on, label) {
  A().isStreaming = on;
  if (on) {
    if (label) A().workingLabel = label;
    startSpinning();
  } else {
    stopSpinning();
  }
}

export function setWorkingLabel(l) { A().workingLabel = l; }

// announce doubles as "commit": previews (arrow-key moves, escape-revert)
// only touch the in-memory setting the spin loop reads; the TUI likewise
// persists workingAnim only when a choice is confirmed.
export function setLoader(name, announce) {
  if (!A().settings) return;
  A().settings.UI.WorkingAnim = name;
  if (announce) {
    saveSettings();
    showNotice('Working animation set to ' + name + '.');
  }
}

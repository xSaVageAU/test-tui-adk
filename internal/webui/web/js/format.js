// format.js — leaf module: presentation helpers with no app state.
// Nothing here may import another app module; everything else is free to
// import this.

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

export function buildBootArt() {
  const rows = Array(5).fill('');
  for (const ch of 'AGENT') {
    const g = BOOT_GLYPHS[ch]; if (!g) continue;
    for (let r = 0; r < 5; r++) {
      for (const c of g[r]) rows[r] += c === '#' ? '██' : '  ';
      rows[r] += '  '; // letter gap
    }
  }
  return rows.join('\n');
}

// ══════════════════════════════════════════════════════════════════
//  Small formatters
// ══════════════════════════════════════════════════════════════════
export function onOff(v)    { return v ? 'on' : 'off'; }
export function fmtDate(iso){ try { return new Date(iso).toLocaleString(); } catch { return iso || ''; } }
export function fmtMs(ms)   { return ms >= 1000 ? (ms/1000).toFixed(1)+'s' : ms+'ms'; }
export function humanK(n)   { return n >= 1e6 ? (n/1e6).toFixed(1)+'M' : n >= 1e3 ? Math.round(n/1e3)+'k' : String(n); }
export function shortId(id) {
  const s = (id || '').replace(/-/g,'');
  return 'sess_' + (s.length > 8 ? s.slice(0,8) : s || '????????');
}

// ══════════════════════════════════════════════════════════════════
//  Scroll
// ══════════════════════════════════════════════════════════════════
export function scrollBottom() {
  const el = document.getElementById('chat-area');
  if (el) el.scrollTop = el.scrollHeight;
}

// ══════════════════════════════════════════════════════════════════
//  Markdown
// ══════════════════════════════════════════════════════════════════
export function renderMarkdown(text) {
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

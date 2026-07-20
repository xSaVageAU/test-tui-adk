// theme.js — theme loading and application. Themes come from the same
// JSON files the TUI uses (/api/themes); applying one maps the 14-token
// palette onto CSS custom properties, so all page styling keys off
// var(--…) and never hardcodes colors.

import { A, showNotice } from './store.js';
import { getThemes } from './api.js';

export async function loadThemes() {
  try {
    const list = await getThemes();
    A().themes = list;
    applyTheme(localStorage.getItem('webui-theme') || list[0]?.name || '', false);
  } catch (e) { console.error('loadThemes', e); }
}

export function applyTheme(name, announce) {
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
  if (announce) showNotice('Theme set to ' + name + '.');
}

export function getThemeColors() {
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

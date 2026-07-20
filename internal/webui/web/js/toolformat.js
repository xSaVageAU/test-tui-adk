// toolformat.js — tool output formatting, a port of
// internal/ui/toolformat.go. Lean and verbose deliberately show
// categorically different things (a bare count/size vs the actual
// content), not the same JSON at two lengths; keep any change here in
// lockstep with the Go file. Everything except effectiveToolPreviewLines
// is pure so the function-to-function mapping with the Go side stays
// obvious.

import { A } from './store.js';

const ARG_PREVIEW_MAX = 60;             // maxArgValuePreview
const TOOL_PREVIEW_LINES_DEFAULT = 25;  // toolPreviewMaxLinesDefault

export function effectiveToolPreviewLines() {
  const n = A().settings?.UI?.ToolPreviewMaxLines || 0;
  return n > 0 ? n : TOOL_PREVIEW_LINES_DEFAULT;
}

// Go's fmt.Sprint equivalent for list entries / arg values — objects get
// JSON rather than "[object Object]".
function anyToStr(v) {
  return typeof v === 'string' ? v : JSON.stringify(v);
}

function truncatePreview(s, max) {
  const flat = String(s).split(/\s+/).filter(Boolean).join(' ');
  if (flat.length <= max) return flat;
  return flat.slice(0, max) + '… (' + flat.length + ' chars)';
}

function truncateLines(s, maxLines) {
  const lines = String(s).split('\n');
  if (lines.length <= maxLines) return s;
  return lines.slice(0, maxLines).join('\n') + '\n… (' + (lines.length - maxLines) + ' more lines)';
}

function humanBytes(n) {
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + ' MB';
  if (n >= 1 << 10) return (n / (1 << 10)).toFixed(1) + ' KB';
  return n + ' bytes';
}

function intFromAny(v) {
  return typeof v === 'number' && Number.isFinite(v) ? Math.trunc(v) : null;
}

function formatValue(v) {
  if (Array.isArray(v)) return v.map(anyToStr).join(',');
  return truncatePreview(anyToStr(v), ARG_PREVIEW_MAX);
}

function formatKV(m) {
  const keys = Object.keys(m || {}).sort();
  return keys.map(k => k + '=' + formatValue(m[k])).join(' ');
}

// Generic result fallback (Go summarizeResult): a single-string value
// renders bare as prose, a single-list value as a count + the list,
// anything else as key=value pairs.
function summarizeResultGeneric(result) {
  const keys = Object.keys(result || {});
  if (keys.length === 1) {
    const k = keys[0], v = result[k];
    if (typeof v === 'string') return v.trim();
    if (Array.isArray(v)) return v.length + ' ' + k + ' — ' + v.map(anyToStr).join(', ');
  }
  return formatKV(result);
}

export function formatToolArgs(name, args) {
  args = args || {};
  switch (name) {
    case 'list_files':
      return (typeof args.path === 'string' && args.path) ? args.path : '.';
    case 'read_file': case 'write_file': case 'edit_file':
      if (typeof args.path === 'string' && args.path) return args.path;
      break;
    case 'grep': case 'glob':
      if (typeof args.pattern === 'string' && args.pattern) return args.pattern;
      break;
    case 'run_shell':
      if (typeof args.command === 'string' && args.command) return truncatePreview(args.command, ARG_PREVIEW_MAX);
      break;
    case 'shell_output': case 'stop_shell':
      if (typeof args.shell_id === 'string' && args.shell_id) return args.shell_id;
      break;
  }
  return formatKV(args);
}

export function formatToolResult(name, args, result, verbose, maxLines) {
  args = args || {}; result = result || {};
  switch (name) {
    case 'read_file':
      if (typeof result.content === 'string') {
        // .length is UTF-16 units, not bytes like Go's len() — close
        // enough for a size label without re-encoding on every render.
        if (!verbose) return 'read ' + humanBytes(result.content.length);
        return truncateLines(result.content, maxLines);
      }
      break;
    case 'write_file': {
      if (!verbose) {
        const n = intFromAny(result.bytesWritten);
        if (n !== null) return 'wrote ' + humanBytes(n);
      } else if (typeof args.content === 'string') {
        // The result only carries bytesWritten — the written content
        // itself lives in the call's args (same as the TUI).
        return truncateLines(args.content, maxLines);
      }
      break;
    }
    case 'list_files':
      if (Array.isArray(result.files) && !verbose) return result.files.length + ' entries';
      break; // verbose falls through to the generic, which lists every name
    case 'edit_file': {
      const n = intFromAny(result.replacements);
      if (n !== null) return n + ' replacements';
      break;
    }
    case 'grep':
      if (Array.isArray(result.matches)) {
        if (!verbose) return result.matches.length + ' matches';
        return truncateLines(result.matches.map(anyToStr).join('\n'), maxLines);
      }
      break;
    case 'glob':
      if (Array.isArray(result.files)) {
        if (!verbose) return result.files.length + ' files';
        return truncateLines(result.files.map(anyToStr).join('\n'), maxLines);
      }
      break;
    case 'run_shell':
      // A background launch returns a shell_id and no meaningful exit code.
      if (typeof result.shell_id === 'string' && result.shell_id) return 'background ' + result.shell_id;
      if (!verbose) {
        const code = intFromAny(result.exit_code);
        if (code !== null) return 'exit ' + code;
      } else if (typeof result.output === 'string') {
        return truncateLines(result.output, maxLines);
      }
      break;
    case 'shell_output':
      if (typeof result.status === 'string') {
        if (verbose && typeof result.output === 'string' && result.output.trim() !== '') {
          return result.status + '\n' + truncateLines(result.output, maxLines);
        }
        return result.status;
      }
      break;
    case 'stop_shell':
      if (typeof result.status === 'string') return result.status;
      break;
  }
  return summarizeResultGeneric(result);
}

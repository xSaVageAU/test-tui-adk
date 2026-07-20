// filetree.js — VS Code-style lazy-loading file-tree sidebar. GET
// /api/files?dir=X lists ONE directory of the active execution target's
// cwd (local or SSH), so the sidebar shows the tree the agent's tools
// actually see and never needs a global tree budget: every folder the
// user expands is fetched — complete — on demand. Near-realtime via a
// poll of the root + all expanded dirs while the sidebar is open, plus an
// immediate refresh whenever a tool finishes or the target changes.
//
// Tree state lives in module vars, not the Alpine store: templates only
// ever render fileTreeRows (rebuilt explicitly after each state change),
// so nothing else needs to be reactive.

import { A } from './store.js';
import { listDir } from './api.js';

const FILETREE_POLL_MS = 3000;

let fileTreePoll = null;
let ftChildren   = {};  // dir path → [{Name, Dir}] (only visible/expanded dirs cached)
let ftExpanded   = {};  // set of expanded dir paths — everything starts collapsed
let ftTruncated  = {};  // dir path → its single-dir listing hit the per-dir cap
let lastTreeJSON = '';

export function toggleFiletree() {
  const on = !A().filetreeOpen;
  A().filetreeOpen = on;
  if (on) {
    refreshFileTree();
    fileTreePoll = setInterval(refreshFileTree, FILETREE_POLL_MS);
  } else if (fileTreePoll) {
    clearInterval(fileTreePoll);
    fileTreePoll = null;
  }
}

// refreshFileTree re-lists everything currently on screen — the root
// plus every expanded directory — in parallel. A root change (target
// switch) resets the whole tree; otherwise the fresh listings replace
// the cache wholesale, which is also what keeps it bounded: collapsed
// directories drop out and simply refetch on next expand.
export async function refreshFileTree() {
  if (!A().filetreeOpen) return;
  try {
    const dirs = ['', ...Object.keys(ftExpanded)];
    const results = await Promise.all(dirs.map(listDir));
    const root = results[0]?.Root || '';
    if (root !== A().fileTreeRoot) {
      A().fileTreeRoot = root;
      ftExpanded = {};
      ftChildren = { '': results[0]?.Entries || [] };
      ftTruncated = { '': !!results[0]?.Truncated };
      lastTreeJSON = '';
      rebuildFtRows();
      return;
    }
    const j = JSON.stringify(results);
    if (j === lastTreeJSON) return; // nothing changed — skip the re-render
    lastTreeJSON = j;
    ftChildren = {};
    ftTruncated = {};
    results.forEach((d, i) => {
      ftChildren[dirs[i]] = d.Entries || [];
      ftTruncated[dirs[i]] = !!d.Truncated;
    });
    pruneFtExpanded();
    rebuildFtRows();
  } catch (e) { console.error('refreshFileTree', e); }
}

// pruneFtExpanded drops expansion state for directories that no longer
// exist in their (freshly fetched) parent listing — a folder deleted on
// disk shouldn't linger as a phantom expanded path being re-polled.
function pruneFtExpanded() {
  for (const p of Object.keys(ftExpanded)) {
    const i = p.lastIndexOf('/');
    const parent = i < 0 ? '' : p.slice(0, i);
    const name   = i < 0 ? p  : p.slice(i + 1);
    const list = ftChildren[parent];
    if (list && !list.some(e => e.Dir && e.Name === name)) {
      delete ftExpanded[p];
      delete ftChildren[p];
      delete ftTruncated[p];
    }
  }
}

export async function ftToggle(path) {
  if (ftExpanded[path]) {
    // Collapse only — cached children and any nested expansion state
    // stay, so re-expanding restores the previous view (VS Code does
    // the same).
    delete ftExpanded[path];
    rebuildFtRows();
    return;
  }
  ftExpanded[path] = true;
  if (!ftChildren[path]) {
    try {
      const d = await listDir(path);
      ftChildren[path] = d.Entries || [];
      ftTruncated[path] = !!d.Truncated;
    } catch (e) { console.error('ftToggle', e); }
  }
  rebuildFtRows();
}

// rebuildFtRows flattens the cached listings into the visible row list,
// depth-first through expanded directories only.
function rebuildFtRows() {
  const rows = [];
  const walk = (dir, depth) => {
    for (const e of (ftChildren[dir] || [])) {
      const path = dir ? dir + '/' + e.Name : e.Name;
      rows.push({ path, name: e.Name, dir: !!e.Dir, depth, expanded: !!ftExpanded[path], note: false });
      if (e.Dir && ftExpanded[path]) walk(path, depth + 1);
    }
    if (ftTruncated[dir]) {
      rows.push({ path: dir + '//truncated', name: '… (listing truncated)', dir: false, depth, expanded: false, note: true });
    }
  };
  walk('', 0);
  A().fileTreeRows = rows;
}

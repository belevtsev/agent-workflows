#!/usr/bin/env node
// agent-workflows distribution patch: no upstream update checks or cache writes.
import { fileURLToPath } from 'node:url';
import { sameEntry } from '../renderers/shared/path-semantics.mjs';

function managed() {
  return { status: 'silent', reason: 'managed-by-agent-workflows' };
}

export async function checkForUpdate(_options = {}) { return managed(); }
export async function setUpdatePreference(_options = {}) { return managed(); }

export function isMainModule({
  argvPath = process.argv[1], modulePath = fileURLToPath(import.meta.url),
} = {}) {
  if (!argvPath) return false;
  try { return sameEntry(argvPath, modulePath).status === 'match'; }
  catch { return false; }
}

if (isMainModule()) process.stdout.write(`${JSON.stringify(managed())}\n`);

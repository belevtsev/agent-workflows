// agent-workflows distribution patch: resource scripts write only externally.
import { resolveNativeOutputDirectory } from '../renderers/shared/output-path.mjs';

export function parseExternalOutputDirectory(args, { allowCheck = false } = {}) {
  let rawDirectory;
  let check = false;
  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index];
    if (allowCheck && arg === '--check' && !check) { check = true; continue; }
    if (arg === '--output-dir' && rawDirectory === undefined) {
      rawDirectory = args[++index];
      if (!rawDirectory || rawDirectory.startsWith('--')) throw new Error('--output-dir requires a directory.');
    } else if (arg.startsWith('--output-dir=') && rawDirectory === undefined) {
      rawDirectory = arg.slice('--output-dir='.length);
      if (!rawDirectory) throw new Error('--output-dir requires a directory.');
    } else throw new Error(`Unknown or duplicate option: ${arg}`);
  }
  if (check) {
    if (rawDirectory !== undefined) throw new Error('--check does not accept --output-dir; it only reads the bundled generated file.');
    return { check: true, outputDirectory: undefined };
  }
  if (rawDirectory === undefined) throw new Error('An explicit external --output-dir is required.');
  return { check: false, outputDirectory: resolveNativeOutputDirectory(rawDirectory) };
}

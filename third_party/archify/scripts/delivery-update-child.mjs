#!/usr/bin/env node
// agent-workflows distribution patch: a direct invocation is also a no-op.
import { checkForUpdate } from './check-update.mjs';
process.stdout.write(`${JSON.stringify(await checkForUpdate())}\n`);

// agent-workflows distribution patch: updates belong to the managed package.
// Keep the upstream unavailable receipt shape without spawning a checker.
export function startDeliveryUpdateCheck(_options = {}) {
  return Promise.resolve({
    status: 'unavailable', installedVersion: null, availableVersion: null,
    releaseNotes: null, checkedAt: null, source: null, noticeRequired: false,
    noticeText: null, reason: 'managed-by-agent-workflows',
  });
}

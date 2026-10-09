import type { Router } from 'vue-router'

const SETTINGS_PATH = '/platform/settings'
const FALLBACK_PATH = '/platform/knowledge-bases'

function settingsReturnPath(router: Router, candidate: unknown): string | undefined {
  if (typeof candidate !== 'string' || !candidate.startsWith('/platform/')) return
  const resolved = router.resolve(candidate)
  // Reject settings itself and legacy redirect entries (tenant/system/integrations)
  // so closing cannot lead straight back into the modal.
  if (resolved.path.replace(/\/$/, '') === SETTINGS_PATH || !resolved.matched.length) return
  if (resolved.matched.at(-1)?.redirect) return
  return resolved.fullPath
}

/** One controller per global Settings host; section changes keep the original return page. */
export function createSettingsNavigation(router: Router) {
  let returnPath = settingsReturnPath(router, router.options.history.state.back) ?? FALLBACK_PATH
  let pendingClose: ReturnType<Router['replace']> | undefined

  const dispose = router.afterEach((to, from, failure) => {
    if (!failure && to.path === SETTINGS_PATH && from.path !== SETTINGS_PATH) {
      returnPath = settingsReturnPath(router, from.fullPath) ?? returnPath
    }
  })

  const close = () => {
    if (pendingClose) return pendingClose
    if (router.currentRoute.value.path !== SETTINGS_PATH) return Promise.resolve()
    // A direct link may have no previous entry, or several settings entries may
    // be consecutive. Explicit replacement works in both cases, unlike back().
    pendingClose = router.replace(returnPath).finally(() => {
      pendingClose = undefined
    })
    return pendingClose
  }

  return { close, dispose, isClosing: () => pendingClose !== undefined }
}

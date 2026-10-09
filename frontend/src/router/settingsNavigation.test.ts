import assert from 'node:assert/strict'
import test from 'node:test'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createSettingsNavigation } from './settingsNavigation'

const SETTINGS = '/platform/settings'
const HOME = '/platform/knowledge-bases'

function setup(back?: string) {
  const history = createMemoryHistory()
  const component = { render: () => null }
  const router = createRouter({ history, routes: [
    { path: SETTINGS, component },
    { path: HOME, component },
    { path: '/platform/agents', component },
    { path: '/platform/tenant', redirect: SETTINGS },
    { path: '/platform/system', redirect: `${SETTINGS}?section=system-global` },
    { path: '/platform/integrations', redirect: `${SETTINGS}?section=integration-api` },
    { path: '/login', component },
    { path: '/platform', component, redirect: HOME, children: [
      { path: 'knowledge-bases/:id', component },
    ] },
  ] })
  if (back) history.state.back = back
  const navigation = createSettingsNavigation(router)
  return { router, navigation }
}

test('a directly opened settings link closes without a browser back entry', async () => {
  const { router, navigation } = setup()
  await router.push(`${SETTINGS}?section=members`)
  await navigation.close()
  assert.equal(router.currentRoute.value.fullPath, HOME)
  navigation.dispose()
})

test('closing skips consecutive settings entries and preserves the original query/hash', async () => {
  const { router, navigation } = setup()
  const origin = '/platform/agents?view=mine#recent'
  await router.push(origin)
  await router.push(`${SETTINGS}?section=general`)
  await router.push(`${SETTINGS}?section=members`)
  await router.replace(`${SETTINGS}?section=models`)
  await navigation.close()
  assert.equal(router.currentRoute.value.fullPath, origin)

  await router.push(HOME)
  await router.push(`${SETTINGS}?section=system-global`)
  await navigation.close()
  assert.equal(router.currentRoute.value.fullPath, HOME)
  navigation.dispose()
})

test('a refreshed settings route can restore a valid history return page', async () => {
  const origin = '/platform/agents?view=mine#recent'
  const { router, navigation } = setup(origin)
  await router.push(`${SETTINGS}?section=members`)
  await navigation.close()
  assert.equal(router.currentRoute.value.fullPath, origin)
  navigation.dispose()
})

test('a detail page remains a valid return target when its parent has a default redirect', async () => {
  const { router, navigation } = setup()
  const origin = '/platform/knowledge-bases/kb-1?folder=docs#file'
  await router.push(origin)
  await router.push(`${SETTINGS}?section=members`)
  await navigation.close()
  assert.equal(router.currentRoute.value.fullPath, origin)
  navigation.dispose()
})

test('settings, redirect, login and external history entries use the home fallback', async () => {
  for (const back of [
    `${SETTINGS}?section=general`, `${SETTINGS}/?section=members`,
    '/platform/tenant', '/platform/system', '/platform/integrations',
    '/login', 'https://example.invalid/', '//example.invalid/platform/agents',
  ]) {
    const { router, navigation } = setup(back)
    await router.push(`${SETTINGS}?section=members`)
    await navigation.close()
    assert.equal(router.currentRoute.value.fullPath, HOME, back)
    navigation.dispose()
  }
})

test('close wins over a pending section change and repeated close calls share the navigation', async () => {
  const { router, navigation } = setup()
  await router.push(HOME)
  await router.push(`${SETTINGS}?section=members`)
  const sectionChange = router.replace(`${SETTINGS}?section=models`)
  const firstClose = navigation.close()
  assert.equal(navigation.isClosing(), true)
  assert.equal(navigation.close(), firstClose)
  await Promise.all([sectionChange, firstClose])
  assert.equal(router.currentRoute.value.fullPath, HOME)
  assert.equal(navigation.isClosing(), false)
  navigation.dispose()
})

test('closing a store-only modal does not navigate away from its underlying page', async () => {
  const { router, navigation } = setup()
  const origin = '/platform/agents?view=mine'
  await router.push(origin)
  await navigation.close()
  assert.equal(router.currentRoute.value.fullPath, origin)
  navigation.dispose()
})

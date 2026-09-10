import assert from 'node:assert/strict'
import test from 'node:test'

import {
  buildSettingsRouteQuery,
  integrationSectionKey,
  isIntegrationSection,
  normalizeSettingsSection,
  settingsQueryUnchanged,
} from './settingsRoute'

test('every settings nav item writes only section', () => {
  assert.deepEqual(
    buildSettingsRouteQuery('models', {
      section: 'system-global',
      tab: 'im',
      agentId: 'agt_1',
    }),
    { section: 'models' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery('general', { section: 'system-global' }),
    { section: 'general' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery('runtime-queues', { section: 'system-global' }),
    { section: 'runtime-queues' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery(integrationSectionKey('claw'), {
      section: 'integrations',
      tab: 'im',
    }),
    { section: 'general' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery(integrationSectionKey('api'), {
      section: 'integrations',
      tab: 'im',
      agentId: 'agt_1',
    }),
    { section: 'integration-api', agentId: 'agt_1' },
  )
})

test('legacy api / integrations / bare-tab query strings normalize to nav keys', () => {
  assert.equal(normalizeSettingsSection('api'), 'integration-api')
  assert.equal(normalizeSettingsSection('claw'), 'general')
  assert.equal(normalizeSettingsSection('integrations', 'embed'), 'general')
  assert.equal(normalizeSettingsSection('integrations'), 'integration-api')
  assert.equal(normalizeSettingsSection('integration-chrome'), 'general')
  assert.equal(normalizeSettingsSection('system-global'), 'system-global')
  assert.equal(isIntegrationSection('integration-chrome'), true)
  assert.equal(isIntegrationSection('models'), false)
  assert.equal(isIntegrationSection('integration-unknown'), false)
})

test('canonical settings query skips a redundant replace', () => {
  assert.equal(
    settingsQueryUnchanged(
      { section: 'integration-claw' },
      { section: 'integration-claw' },
    ),
    true,
  )
  assert.equal(
    settingsQueryUnchanged(
      { section: 'integrations', tab: 'claw' },
      { section: 'integration-claw' },
    ),
    false,
  )
})


test('hidden settings links and legacy aliases fall back without retaining integration context', () => {
  const hidden = [
    'envvars', 'ollama', 'weknoracloud', 'sandbox', 'skills', 'websearch', 'mcp', 'system',
    'im', 'embed', 'chrome', 'claw',
    'integration-im', 'integration-embed', 'integration-chrome', 'integration-claw',
  ]
  for (const section of hidden) {
    assert.equal(normalizeSettingsSection(section), 'general', section)
    assert.deepEqual(buildSettingsRouteQuery(section, { tab: 'im', agentId: 'agent-1' }), {
      section: 'general',
    }, section)
  }
  assert.equal(normalizeSettingsSection('integrations', 'api'), 'integration-api')
  assert.equal(normalizeSettingsSection('parser'), 'parser')
})

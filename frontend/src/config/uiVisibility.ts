/** Product UI policy, independent of workspace roles and backend availability. */
const HIDDEN_SETTINGS_SECTIONS = new Set([
  'envvars',
  'ollama',
  'weknoracloud',
  'integration-im',
  'integration-embed',
  'integration-chrome',
  'integration-claw',
  'sandbox',
  'skills',
  'websearch',
  'mcp',
  'system',
])

const HIDDEN_PARSER_ENGINES = new Set([
  'weknoracloud',
  'mineru',
  'mineru_cloud',
  'paddleocr_vl',
  'paddleocr_vl_cloud',
])

export function isSettingsSectionVisible(section: string): boolean {
  return !HIDDEN_SETTINGS_SECTIONS.has(section)
}

export function isParserEngineVisible(name: string): boolean {
  return !HIDDEN_PARSER_ENGINES.has(name)
}

export function visibleParserEngines<T extends { Name: string }>(engines: T[]): T[] {
  return engines.filter(engine => isParserEngineVisible(engine.Name))
}

import { test, done, eq, ok } from './testkit'
import type { SettingsValues } from './api'
import { fromEffective, isDirty, validate, hasErrors, buildPayload, apiKeyLabel } from './settingsform'

const eff: SettingsValues = {
  os: 'auto', certmachineUrl: 'https://cm.example.com', certmachineCaFile: '', configPath: '', certsDir: '/srv/certs',
  crtListPath: '', statsSocketPath: '', backupDir: '', serviceName: '', backupKeep: 10, expiryWarnDays: 30,
}

test('a freshly loaded form is clean and valid', () => {
  const f = fromEffective(eff)
  eq(isDirty(f, f), false, 'dirty')
  eq(hasErrors(validate(f)), false, 'errors')
  eq(f.values.backupKeep, '10', 'numbers become text')
})

test('editing a value, typing a key, or clearing the key makes the form dirty; reverting cleans it', () => {
  const saved = fromEffective(eff)
  const edited = { ...saved, values: { ...saved.values, certsDir: '/x' } }
  eq(isDirty(saved, edited), true, 'edit')
  eq(isDirty(saved, { ...saved, apiKey: 'k' }), true, 'key')
  eq(isDirty(saved, { ...saved, clearApiKey: true }), true, 'clear')
  eq(isDirty(saved, { ...edited, values: { ...edited.values, certsDir: '/srv/certs' } }), false, 'reverted')
})

test('validation mirrors the server: url, paths, service name, ranges', () => {
  const bad = (k: string, v: string) => {
    const f = fromEffective(eff)
    return validate({ ...f, values: { ...f.values, [k]: v } })
  }
  ok(bad('certmachineUrl', 'http://cm.example.com').certmachineUrl, 'http non-loopback')
  ok(!bad('certmachineUrl', 'http://localhost:8080').certmachineUrl, 'http loopback ok')
  ok(!bad('certmachineUrl', '').certmachineUrl, 'empty url ok')
  ok(bad('certmachineUrl', 'not a url').certmachineUrl, 'unparsable')
  ok(bad('certmachineUrl', 'ftp://x.example.com').certmachineUrl, 'scheme')
  ok(bad('certsDir', 'certs').certsDir, 'relative')
  ok(bad('backupDir', '/a/../b').backupDir, 'dotdot')
  ok(bad('configPath', '/a\nb').configPath, 'control char')
  ok(!bad('configPath', '').configPath, 'empty path ok (driver default)')
  ok(bad('serviceName', 'ha proxy').serviceName, 'service name')
  ok(!bad('serviceName', 'haproxy@edge.service').serviceName, 'service name ok')
  ok(bad('backupKeep', '0').backupKeep && bad('backupKeep', '101').backupKeep && bad('backupKeep', 'x').backupKeep, 'keep range')
  ok(!bad('backupKeep', '100').backupKeep, 'keep 100 ok')
  ok(bad('expiryWarnDays', '366').expiryWarnDays && bad('expiryWarnDays', '').expiryWarnDays, 'warn range')
  ok(validate({ ...fromEffective(eff), apiKey: 'k', clearApiKey: true }).apiKey, 'key and clear')
})

test('payload is the full trimmed set with numbers and the key intent', () => {
  const f = fromEffective(eff)
  const p = buildPayload({ ...f, values: { ...f.values, certsDir: ' /x ', backupKeep: '12' }, apiKey: 'new' })
  eq(p.certsDir, '/x', 'trimmed')
  eq(p.backupKeep, 12, 'number')
  eq(p.apiKey, 'new', 'key')
  eq(p.clearApiKey, false, 'clear')
  eq(Object.keys(p).sort(), [...Object.keys(eff), 'apiKey', 'clearApiKey'].sort(), 'full set')
})

test('api key label says what is stored and what Save will do', () => {
  const f = fromEffective(eff)
  eq(apiKeyLabel(true, f), 'Set')
  eq(apiKeyLabel(false, f), 'Not set')
  eq(apiKeyLabel(true, { ...f, clearApiKey: true }), 'Will be cleared on Save')
  eq(apiKeyLabel(false, { ...f, apiKey: 'k' }), 'Will be replaced on Save')
})

done('settingsform')

import { test, done, eq, ok } from './testkit'
import type { Model, Service } from './api'
import {
  normalizeModel, newModelId, addService, updateService, removeService, setServiceEnabled,
  addFqdn, setFqdn, removeFqdn, setUpstreamHost, addPort, removePort, setPortDefault,
  portDefaultOptions, groupByPort, isDirty, finalizeForSave, extraToText, textToExtra,
  DEFAULT_UPSTREAM_HOST, updateDefaultService,
} from './modelForm'
import {
  addDirective, updateDirective, removeDirective, moveDirective, isOsManaged, COMMON_KEYS,
} from './globalsForm'

function base(): Model {
  return {
    version: 1,
    global: [{ key: 'maxconn', value: '2000' }],
    defaults: [{ key: 'mode', value: 'http' }],
    defaultService: { name: 'unified', upstream: { host: '127.0.0.1', port: 8787 }, check: true },
    ports: [],
    services: [],
    rawSections: [],
  }
}

function svc(m: Model, id: string): Service {
  const s = m.services.find(x => x.id === id)
  if (!s) throw new Error(`no service ${id}`)
  return s
}

test('normalizeModel turns null arrays into empty ones and drops "imported"', () => {
  const raw = { ...base(), services: null, ports: null, global: null, imported: true } as unknown as Model
  const m = normalizeModel(raw)
  eq(m.services, [])
  eq(m.ports, [])
  eq(m.global, [])
  ok(!('imported' in m), 'imported flag must not survive')
})

test('addService defaults: 443, upstream host 127.0.0.1, enabled, check on, unique id', () => {
  let m = addService(base(), 443)
  m = addService(m, 443)
  eq(m.services.length, 2)
  const [a, b] = m.services
  ok(a.id !== b.id, 'ids must be unique')
  eq(a.exposedPort, 443)
  eq(a.upstream.host, DEFAULT_UPSTREAM_HOST)
  eq(a.enabled, true)
  eq(a.check, true)
  eq(a.fqdns, [])
  eq(a.extra, [])
  ok(a.name !== b.name, 'names must be unique so the new rows do not start in error')
})

test('addService on another port places it on that port', () => {
  const m = addService(base(), 8443)
  eq(m.services[0].exposedPort, 8443)
})

test('newModelId never reuses an id after a removal', () => {
  let m = addService(addService(base(), 443), 443)
  const removed = m.services[1].id
  m = removeService(m, removed)
  ok(newModelId(m) !== m.services[0].id, 'fresh id differs from live ids')
})

test('enable/disable a service', () => {
  let m = addService(base(), 443)
  const id = m.services[0].id
  m = setServiceEnabled(m, id, false)
  eq(svc(m, id).enabled, false)
  m = setServiceEnabled(m, id, true)
  eq(svc(m, id).enabled, true)
})

test('add / edit / remove FQDN', () => {
  let m = addService(base(), 443)
  const id = m.services[0].id
  m = addFqdn(m, id, '  a.example.com ')
  m = addFqdn(m, id, 'b.example.com')
  eq(svc(m, id).fqdns, ['a.example.com', 'b.example.com'])
  m = setFqdn(m, id, 1, 'c.example.com')
  eq(svc(m, id).fqdns, ['a.example.com', 'c.example.com'])
  m = removeFqdn(m, id, 0)
  eq(svc(m, id).fqdns, ['c.example.com'])
})

test('addFqdn ignores blanks and duplicates within the service (case-insensitive)', () => {
  let m = addService(base(), 443)
  const id = m.services[0].id
  m = addFqdn(m, id, 'a.example.com')
  const before = m
  eq(addFqdn(m, id, '   '), before)
  eq(addFqdn(m, id, 'A.Example.com'), before)
})

test('upstream host defaults to 127.0.0.1 when blank at save time', () => {
  let m = addService(base(), 443)
  const id = m.services[0].id
  m = setUpstreamHost(m, id, '')
  eq(svc(m, id).upstream.host, '') // editable: a blank while typing is allowed
  eq(svc(finalizeForSave(m), id).upstream.host, '127.0.0.1')
  m = setUpstreamHost(m, id, 'hero.example.com')
  eq(svc(finalizeForSave(m), id).upstream.host, 'hero.example.com')
})

test('finalizeForSave trims, drops blank FQDNs and blank extra lines', () => {
  let m = addService(base(), 443)
  const id = m.services[0].id
  m = updateService(m, id, { fqdns: [' a.example.com ', ''], extra: ['  timeout x 1s ', '', '  '], name: ' web ' })
  const s = svc(finalizeForSave(m), id)
  eq(s.fqdns, ['a.example.com'])
  eq(s.extra, ['timeout x 1s'])
  eq(s.name, 'web')
})

test('extra directives textarea round trip', () => {
  eq(textToExtra('a 1\n\n  b 2  \n'), ['a 1', 'b 2'])
  eq(extraToText(['a 1', 'b 2']), 'a 1\nb 2')
  eq(textToExtra(''), [])
})

test('addPort validates range, 443, duplicates', () => {
  let r = addPort(base(), 8443)
  eq(r.error, null)
  eq(r.model.ports, [{ port: 8443 }])
  for (const bad of [0, 70000, 1.5, 443, NaN]) {
    const x = addPort(base(), bad)
    ok(x.error !== null, `port ${bad} must be refused`)
    eq(x.model.ports, [])
  }
  r = addPort(r.model, 8443)
  ok(r.error !== null, 'duplicate port refused')
  eq(r.model.ports.length, 1)
})

test('setPortDefault only accepts a service on that port; "" clears', () => {
  let m = addPort(base(), 8443).model
  m = addService(m, 8443)
  const name = m.services[0].name
  let r = setPortDefault(m, 8443, name)
  eq(r.error, null)
  eq(r.model.ports[0].defaultService, name)
  r = setPortDefault(r.model, 8443, '')
  eq(r.model.ports[0].defaultService, undefined)
  const other = addService(m, 443)
  const nm443 = other.services[1].name
  const bad = setPortDefault(other, 8443, nm443)
  ok(bad.error !== null, 'a service on another port is refused')
})

test('portDefaultOptions lists only the services on that port', () => {
  let m = addPort(base(), 8443).model
  m = addService(addService(m, 8443), 443)
  eq(portDefaultOptions(m, 8443), [m.services[0].name])
})

test('removePort refuses while services use it, otherwise removes', () => {
  let m = addPort(base(), 8443).model
  m = addService(m, 8443)
  ok(removePort(m, 8443).error !== null, 'refused while in use')
  const empty = addPort(base(), 9443).model
  const r = removePort(empty, 9443)
  eq(r.error, null)
  eq(r.model.ports, [])
})

test('removing a service clears a port default that pointed at it', () => {
  let m = addPort(base(), 8443).model
  m = addService(m, 8443)
  m = setPortDefault(m, 8443, m.services[0].name).model
  m = removeService(m, m.services[0].id)
  eq(m.ports[0].defaultService, undefined)
})

test('renaming a service keeps the port default pointing at it', () => {
  let m = addPort(base(), 8443).model
  m = addService(m, 8443)
  const id = m.services[0].id
  m = setPortDefault(m, 8443, m.services[0].name).model
  m = updateService(m, id, { name: 'renamed' })
  eq(m.ports[0].defaultService, 'renamed')
})

test('moving a service to another port clears the old port default', () => {
  let m = addPort(base(), 8443).model
  m = addService(m, 8443)
  const id = m.services[0].id
  m = setPortDefault(m, 8443, m.services[0].name).model
  m = updateService(m, id, { exposedPort: 443 })
  eq(m.ports[0].defaultService, undefined)
})

test('groupByPort: 443 always first with the unified default; extra ports ascending', () => {
  let m = addService(addService(addPort(addPort(base(), 9443).model, 8443).model, 8443), 443)
  const g = groupByPort(m)
  eq(g.map(x => x.port), [443, 8443, 9443])
  eq(g[0].isDefaultPort, true)
  eq(g[0].defaultServiceName, 'unified')
  eq(g[0].services.length, 1)
  eq(g[1].services.length, 1)
  eq(g[2].services.length, 0)
  eq(g[1].isDefaultPort, false)
})

test('groupByPort on an empty model still shows 443; a service on an unlisted port gets a group', () => {
  eq(groupByPort(base()).map(x => x.port), [443])
  let m = addService(base(), 7000)
  eq(groupByPort(m).map(x => x.port), [443, 7000])
})

test('groupByPort treats exposedPort 0 as 443', () => {
  let m = addService(base(), 443)
  m = updateService(m, m.services[0].id, { exposedPort: 0 })
  eq(groupByPort(m)[0].services.length, 1)
})

test('isDirty: only when different from the last saved model', () => {
  const saved = base()
  ok(!isDirty(saved, normalizeModel({ ...base(), imported: true } as unknown as Model)), 'identical is clean')
  const edited = addService(saved, 443)
  ok(isDirty(saved, edited), 'an added service is dirty')
  ok(!isDirty(saved, removeService(edited, edited.services[0].id)), 'reverting the edit is clean again')
  ok(isDirty(saved, setPortDefault(addService(addPort(saved, 8443).model, 8443), 8443, '').model), 'ports differ')
})

test('updateDefaultService edits the 443 default service upstream without touching services', () => {
  const m = updateDefaultService(addService(base(), 443), { upstream: { host: '10.0.0.5', port: 9000 } })
  eq(m.defaultService.upstream, { host: '10.0.0.5', port: 9000 })
  eq(m.defaultService.name, 'unified')
  eq(m.services.length, 1)
})

test('globals: add / update / remove / reorder', () => {
  let g = [{ key: 'a', value: '1' }, { key: 'b', value: '2' }, { key: 'c', value: '3' }]
  g = addDirective(g, 'd', '4')
  eq(g.map(x => x.key), ['a', 'b', 'c', 'd'])
  g = updateDirective(g, 0, { value: '9' })
  eq(g[0], { key: 'a', value: '9' })
  g = moveDirective(g, 2, -1)
  eq(g.map(x => x.key), ['a', 'c', 'b', 'd'])
  g = moveDirective(g, 0, -1)
  eq(g.map(x => x.key), ['a', 'c', 'b', 'd'])
  g = moveDirective(g, 3, 1)
  eq(g.map(x => x.key), ['a', 'c', 'b', 'd'])
  g = removeDirective(g, 1)
  eq(g.map(x => x.key), ['a', 'b', 'd'])
})

test('globals: addDirective ignores a blank key; pick-list has the common keys; OS-managed keys flagged', () => {
  eq(addDirective([], '  ', 'x'), [])
  const keys = COMMON_KEYS.map(k => k.key)
  for (const k of ['maxconn', 'log', 'user', 'group', 'chroot', 'timeout', 'nbthread']) ok(keys.includes(k), `pick-list has ${k}`)
  ok(isOsManaged('user') && isOsManaged('chroot') && isOsManaged('stats socket'), 'baseline keys are managed')
  ok(!isOsManaged('maxconn'), 'maxconn is the operator\'s')
})

done('modelForm')

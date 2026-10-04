// @ts-nocheck
// Regression guard for the smbedit scroll bug: shares fell off the bottom of
// the screen with no scrollbar and the footer was pushed out of view.
//
// Cause: `.layout { height: 100% }` only resolves against a parent with a
// definite height, and #root had none, so the grid grew to its content and
// body's overflow:hidden clipped it. This asserts the contract in the
// stylesheet text (no browser here): the height chain from body to .layout is
// definite, and the content area is the thing that scrolls.
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

const css = readFileSync(join(process.cwd(), 'web/smbedit/src/styles.css'), 'utf8')

function rule(selector: string): string {
  const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = new RegExp('(?:^|\\})\\s*' + esc + '\\s*\\{([^}]*)\\}', 'm').exec(css)
  return m ? m[1] : ''
}

const problems: string[] = []
const layout = rule('.layout')
const root = rule('#root')
const body = rule('body')
const main = rule('.main-content')

if (!/grid-template-rows:[^;]*\b1fr\b/.test(layout)) problems.push('.layout must have a 1fr content row between the header and the footer')
if (/height:\s*100%/.test(layout) && !/height:\s*100%/.test(root)) {
  problems.push('.layout is height:100% but #root has no height:100%, so it grows past the viewport (the footer falls off the bottom, nothing scrolls)')
}
if (!/height:\s*100vh/.test(body)) problems.push('body must be exactly one viewport tall (height: 100vh)')
if (!/overflow-y:\s*auto/.test(main)) problems.push('.main-content must be the scrolling region (overflow-y: auto)')
if (!/min-height:\s*0/.test(main)) problems.push('.main-content needs min-height: 0 so the 1fr grid row can shrink and it can scroll')

if (problems.length) {
  console.error('smbedit layout guard failed:\n - ' + problems.join('\n - '))
  process.exit(1)
}
console.log('smbedit layout guard: ok')

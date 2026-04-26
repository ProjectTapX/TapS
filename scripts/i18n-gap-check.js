#!/usr/bin/env node
// Compare zh, en, ja translation trees and report missing keys.
const fs = require('fs')
const path = require('path')
const vm = require('vm')

const dir = path.join(__dirname, '../web/src/i18n')

function loadLang(name) {
  let src = fs.readFileSync(path.join(dir, name + '.ts'), 'utf8')
  // Strip "export const xx = " prefix, keep the object literal
  src = src.replace(/^\/\/.*$/gm, '') // strip comments
  src = src.replace(/export\s+const\s+\w+\s*=\s*/, '')
  const ctx = { result: null }
  vm.createContext(ctx)
  vm.runInContext('result = ' + src, ctx)
  return ctx.result
}

function flatten(obj, prefix = '') {
  const out = []
  for (const k of Object.keys(obj)) {
    const v = obj[k]
    const key = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      out.push(...flatten(v, key))
    } else {
      out.push(key)
    }
  }
  return out
}

const langs = ['zh', 'en', 'ja']
const sets = {}
for (const l of langs) {
  sets[l] = new Set(flatten(loadLang(l)))
  console.log(`${l} keys: ${sets[l].size}`)
}
console.log()

let exitCode = 0
for (let i = 0; i < langs.length; i++) {
  for (let j = i + 1; j < langs.length; j++) {
    const a = langs[i], b = langs[j]
    const missingInB = [...sets[a]].filter(k => !sets[b].has(k)).sort()
    const missingInA = [...sets[b]].filter(k => !sets[a].has(k)).sort()
    if (missingInB.length) {
      console.log(`Keys in ${a} but missing in ${b} (${missingInB.length}):`)
      for (const k of missingInB) console.log('  ' + k)
      console.log()
      exitCode = 1
    }
    if (missingInA.length) {
      console.log(`Keys in ${b} but missing in ${a} (${missingInA.length}):`)
      for (const k of missingInA) console.log('  ' + k)
      console.log()
      exitCode = 1
    }
  }
}
process.exit(exitCode)

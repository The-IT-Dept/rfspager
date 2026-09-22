const assert = require("node:assert/strict")
const { spawnSync } = require("node:child_process")
const { createRequire } = require("node:module")

const expoRouterPackage = require.resolve("expo-router/package.json")
const queryString = createRequire(expoRouterPackage)("query-string")

assert.equal(queryString.parse("message=hello+world%21").message, "hello world!")

const child = spawnSync(
  process.execPath,
  [
    "-e",
    [
      "const {createRequire}=require('node:module')",
      "const queryString=createRequire(process.argv[1])('query-string')",
      "const result=queryString.parse('message=' + '%E0%A4'.repeat(25000))",
      "if(typeof result.message !== 'string') process.exit(2)",
    ].join(";"),
    expoRouterPackage,
  ],
  { timeout: 1000 },
)

if (child.error || child.signal || child.status !== 0) {
  throw child.error ?? new Error(`Decoder child failed: status=${child.status} signal=${child.signal}`)
}

console.log("decode-uri-component CJS backport regression passed")

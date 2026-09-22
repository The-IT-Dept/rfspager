const assert = require("node:assert/strict")
const { spawnSync } = require("node:child_process")
const { mkdirSync, rmSync } = require("node:fs")
const { join } = require("node:path")

const output = "/private/tmp/rfspager-session-guard-test"
rmSync(output, { recursive: true, force: true })
mkdirSync(output, { recursive: true })

const compile = spawnSync(
  process.platform === "win32" ? "pnpm.cmd" : "pnpm",
  [
    "exec",
    "tsc",
    "--ignoreConfig",
    "src/stores/session-guard.ts",
    "--target",
    "es2022",
    "--module",
    "node16",
    "--moduleResolution",
    "node16",
    "--skipLibCheck",
    "--outDir",
    output,
  ],
  { stdio: "inherit" },
)
if (compile.status !== 0) process.exit(compile.status ?? 1)

const { captureSession, sessionIsCurrent } = require(join(output, "session-guard.js"))

async function delayedUpsertDoesNotSendForANewerSession() {
  let current = { token: "token-a", userID: "user-a" }
  let resolveInput
  const input = new Promise((resolve) => {
    resolveInput = resolve
  })
  let sends = 0

  const captured = captureSession(current.token, current.userID)
  const task = (async () => {
    await input
    if (captured && sessionIsCurrent(captured, current)) sends++
  })()

  current = { token: "token-b", userID: "user-b" }
  resolveInput()
  await task
  assert.equal(sends, 0)
}

delayedUpsertDoesNotSendForANewerSession()
  .then(() => console.log("auth-store account-switch regression passed"))
  .finally(() => rmSync(output, { recursive: true, force: true }))

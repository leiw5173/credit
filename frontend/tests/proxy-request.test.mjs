import { once } from "node:events"
import { spawn } from "node:child_process"
import { createServer } from "node:net"
import test from "node:test"
import assert from "node:assert/strict"

const frontendRoot = new URL("..", import.meta.url).pathname

async function reservePort() {
  const server = createServer()
  server.listen(0, "127.0.0.1")
  await once(server, "listening")
  const address = server.address()
  if (!address || typeof address === "string") throw new Error("unable to reserve test port")
  await new Promise((resolve) => server.close(resolve))
  return address.port
}

async function startNext(env) {
  const port = await reservePort()
  const server = spawn(process.execPath, ["node_modules/next/dist/bin/next", "start", "-p", String(port)], {
    cwd: frontendRoot,
    env: { ...process.env, ...env },
    stdio: ["ignore", "pipe", "pipe"],
  })
  let output = ""
  server.stdout.on("data", (chunk) => { output += chunk })
  server.stderr.on("data", (chunk) => { output += chunk })

  const baseUrl = `http://127.0.0.1:${port}`
  for (let attempt = 0; attempt < 50; attempt++) {
    try {
      await fetch(`${baseUrl}/login`)
      return { baseUrl, server, output: () => output }
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 100))
    }
  }

  server.kill("SIGTERM")
  throw new Error(`Next server did not start:\n${output}`)
}

async function stopNext(server) {
  server.kill("SIGTERM")
  await once(server, "exit")
}

test("proxy preserves ordinary routes when legacy commerce is disabled", async (t) => {
  const app = await startNext({ LINUX_DO_CREDIT_LEGACY_COMMERCE: "false" })
  t.after(() => stopNext(app.server))

  const response = await fetch(`${app.baseUrl}/login`)
  assert.equal(response.status, 200, app.output())
})

test("proxy returns 404 for disabled legacy commerce pages", async (t) => {
  const app = await startNext({ LINUX_DO_CREDIT_LEGACY_COMMERCE: "false" })
  t.after(() => stopNext(app.server))

  for (const path of ["/merchant", "/paying", "/redenvelope/example"]) {
    const response = await fetch(`${app.baseUrl}${path}`)
    assert.equal(response.status, 404, `${path}: ${app.output()}`)
  }
})

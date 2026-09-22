import assert from "node:assert/strict"
import { execFile } from "node:child_process"
import { promisify } from "node:util"
import test from "node:test"

const run = promisify(execFile)
const frontendRoot = new URL("..", import.meta.url).pathname

test("proxy handles ordinary and disabled-commerce paths outside render scope", async () => {
  try {
    const { stdout } = await run(
      process.execPath,
      ["node_modules/tsx/dist/cli.mjs", "tests/proxy-direct-invoke.ts"],
      {
        cwd: frontendRoot,
        env: {
          ...process.env,
          NODE_OPTIONS: "--conditions=react-server",
          LINUX_DO_CREDIT_LEGACY_COMMERCE: "false",
        },
        timeout: 1000,
      },
    )
    assert.deepEqual(JSON.parse(stdout), [200, 404])
  } catch (error) {
    assert.fail(`proxy did not return the expected request responses: ${error}`)
  }
})

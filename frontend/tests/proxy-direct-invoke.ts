import { NextRequest } from "next/server"
import { proxy } from "../proxy"

async function main() {
  const ordinaryResponse = await proxy(new NextRequest("http://localhost/login"))
  const commerceResponse = await proxy(new NextRequest("http://localhost/merchant"))

  console.log(JSON.stringify([ordinaryResponse.status, commerceResponse.status]))
}

void main().then(
  () => process.exit(0),
  (error) => {
    console.error(error)
    process.exit(1)
  },
)

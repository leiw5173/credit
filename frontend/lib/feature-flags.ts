import "server-only"
import { connection } from "next/server"

export async function legacyCommerceEnabled(): Promise<boolean> {
  await connection()
  return process.env.LINUX_DO_CREDIT_LEGACY_COMMERCE === "true"
}

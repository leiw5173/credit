import { notFound } from "next/navigation"
import { TradeMain } from "@/components/common/trade/trade-main"
import { legacyCommerceEnabled } from "@/lib/feature-flags"

export default async function TradePage() {
  if (!await legacyCommerceEnabled()) notFound()
  return <TradeMain />
}

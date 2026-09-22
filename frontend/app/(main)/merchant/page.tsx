import { notFound } from "next/navigation"
import { MerchantMain } from "@/components/common/merchant/merchant-main"
import { legacyCommerceEnabled } from "@/lib/feature-flags"

export default async function MerchantPage() {
  if (!await legacyCommerceEnabled()) notFound()
  return <MerchantMain />
}

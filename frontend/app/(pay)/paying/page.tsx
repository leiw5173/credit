import { Suspense } from "react"
import { notFound } from "next/navigation"
import { PayingMain } from "@/components/common/pay/paying/paying-main"
import { legacyCommerceEnabled } from "@/lib/feature-flags"

export default async function Page() {
  if (!await legacyCommerceEnabled()) notFound()
  return (
    <Suspense>
      <PayingMain />
    </Suspense>
  )
}

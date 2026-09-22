import { MainLayoutClient } from "@/components/layout/main-layout-client"
import { legacyCommerceEnabled } from "@/lib/feature-flags"

export default async function MainLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <MainLayoutClient legacyCommerceEnabled={await legacyCommerceEnabled()}>
      {children}
    </MainLayoutClient>
  )
}

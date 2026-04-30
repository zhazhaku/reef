import {
  Navigate,
  Outlet,
  createFileRoute,
  useRouterState,
  Link,
} from "@tanstack/react-router"
import { IconDashboard, IconListDetails, IconNetwork } from "@tabler/icons-react"
import { useTranslation } from "react-i18next"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/reef")({
  component: ReefLayout,
})

const tabs = [
  { to: "/reef/overview", label: "reef.overview", icon: IconDashboard },
  { to: "/reef/tasks", label: "reef.tasks", icon: IconListDetails },
  { to: "/reef/clients", label: "reef.clients", icon: IconNetwork },
] as const

function ReefLayout() {
  const { t } = useTranslation()
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  })

  if (pathname === "/reef") {
    return <Navigate to="/reef/overview" />
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-1 border-b px-4 py-2">
        {tabs.map((tab) => (
          <Link
            key={tab.to}
            to={tab.to}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
              pathname.startsWith(tab.to)
                ? "bg-primary/10 text-primary"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <tab.icon className="size-4" />
            {t(tab.label)}
          </Link>
        ))}
      </div>
      <div className="flex-1 overflow-auto p-4">
        <Outlet />
      </div>
    </div>
  )
}

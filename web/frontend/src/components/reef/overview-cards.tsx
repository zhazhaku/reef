import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ReefStatus } from "@/api/reef"
import { IconClock, IconPlayerPlay, IconCheck, IconX, IconPlugConnected } from "@tabler/icons-react"

const stats = [
  { key: "queued_tasks", label: "Queued", icon: IconClock, color: "text-gray-500" },
  { key: "running_tasks", label: "Running", icon: IconPlayerPlay, color: "text-blue-500" },
  { key: "completed_tasks", label: "Completed", icon: IconCheck, color: "text-green-500" },
  { key: "failed_tasks", label: "Failed", icon: IconX, color: "text-red-500" },
  { key: "connected_clients", label: "Connected", icon: IconPlugConnected, color: "text-emerald-500" },
] as const

export function OverviewCards({ status }: { status: ReefStatus }) {
  return (
    <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
      {stats.map(({ key, label, icon: Icon, color }) => (
        <Card key={key}>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              {label}
            </CardTitle>
            <Icon className={`size-4 ${color}`} />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {(status as any)[key] ?? 0}
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

import { Badge } from "@/components/ui/badge"
import { ReefTask } from "@/api/reef"
import { cn } from "@/lib/utils"

const STATUS_STYLES: Record<string, string> = {
  queued: "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300",
  running: "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
  completed: "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
  failed: "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
  cancelled: "bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300",
  escalated: "bg-orange-100 text-orange-700 dark:bg-orange-900 dark:text-orange-300",
}

function statusBadge(status: string) {
  const style = STATUS_STYLES[status] ?? "bg-gray-100 text-gray-700"
  return <Badge className={cn("font-medium capitalize", style)} variant="outline">{status}</Badge>
}

function truncateId(id: string): string {
  if (id.length <= 8) return id
  return id.slice(0, 8) + "..."
}

function formatTime(s: string): string {
  if (!s) return ""
  return new Date(s).toLocaleString()
}

export function TaskTable({ tasks, onSelect }: { tasks: ReefTask[]; onSelect: (task: ReefTask) => void }) {
  if (tasks.length === 0) {
    return <p className="text-muted-foreground text-sm py-8 text-center">No tasks found.</p>
  }

  return (
    <div className="overflow-x-auto rounded-xl border">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/50 text-left">
            <th className="px-4 py-3 font-medium">ID</th>
            <th className="px-4 py-3 font-medium">Instruction</th>
            <th className="px-4 py-3 font-medium">Status</th>
            <th className="px-4 py-3 font-medium">Role</th>
            <th className="px-4 py-3 font-medium">Client</th>
            <th className="px-4 py-3 font-medium">Created</th>
          </tr>
        </thead>
        <tbody>
          {tasks.map((t) => (
            <tr
              key={t.id}
              className="border-b last:border-b-0 hover:bg-muted/50 cursor-pointer transition-colors"
              onClick={() => onSelect(t)}
            >
              <td className="px-4 py-3 font-mono text-xs">{truncateId(t.id)}</td>
              <td className="px-4 py-3 max-w-[300px] truncate">{t.instruction}</td>
              <td className="px-4 py-3">{statusBadge(t.status)}</td>
              <td className="px-4 py-3 capitalize text-muted-foreground">{t.required_role}</td>
              <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                {t.assigned_client || "—"}
              </td>
              <td className="px-4 py-3 text-muted-foreground whitespace-nowrap">
                {formatTime(t.created_at)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

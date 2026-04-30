import { ReefClient } from "@/api/reef"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

const STATE_STYLES: Record<string, string> = {
  connected: "bg-green-100 text-green-700",
  stale: "bg-yellow-100 text-yellow-700",
  disconnected: "bg-red-100 text-red-700",
}

function formatTime(s: string): string {
  if (!s) return "—"
  return new Date(s).toLocaleString()
}

export function ClientTable({ clients }: { clients: ReefClient[] }) {
  if (clients.length === 0) {
    return <p className="text-muted-foreground text-sm py-8 text-center">No clients connected.</p>
  }

  return (
    <div className="overflow-x-auto rounded-xl border">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/50 text-left">
            <th className="px-4 py-3 font-medium">ID</th>
            <th className="px-4 py-3 font-medium">Role</th>
            <th className="px-4 py-3 font-medium">Skills</th>
            <th className="px-4 py-3 font-medium">State</th>
            <th className="px-4 py-3 font-medium">Load</th>
            <th className="px-4 py-3 font-medium">Last Seen</th>
          </tr>
        </thead>
        <tbody>
          {clients.map((c) => (
            <tr key={c.id} className="border-b last:border-b-0 hover:bg-muted/50 transition-colors">
              <td className="px-4 py-3 font-mono text-xs">{c.id}</td>
              <td className="px-4 py-3 capitalize">{c.role}</td>
              <td className="px-4 py-3 text-muted-foreground max-w-[200px] truncate">
                {c.skills?.join(", ") || "—"}
              </td>
              <td className="px-4 py-3">
                <Badge
                  className={cn("capitalize font-medium", STATE_STYLES[c.state] ?? "bg-gray-100 text-gray-700")}
                  variant="outline"
                >
                  {c.state}
                </Badge>
              </td>
              <td className="px-4 py-3">
                <div className="flex items-center gap-2">
                  <div className="h-2 flex-1 rounded-full bg-muted max-w-[120px]">
                    <div
                      className="h-2 rounded-full bg-primary transition-all"
                      style={{ width: `${c.capacity > 0 ? Math.round((c.current_load / c.capacity) * 100) : 0}%` }}
                    />
                  </div>
                  <span className="text-xs text-muted-foreground">
                    {c.current_load}/{c.capacity}
                  </span>
                </div>
              </td>
              <td className="px-4 py-3 text-muted-foreground whitespace-nowrap">
                {formatTime(c.last_seen_at)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

import { ReefTask } from "@/api/reef"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet"
import { cn } from "@/lib/utils"
import { IconX } from "@tabler/icons-react"

const STATUS_STYLES: Record<string, string> = {
  queued: "bg-gray-100 text-gray-700",
  running: "bg-blue-100 text-blue-700",
  completed: "bg-green-100 text-green-700",
  failed: "bg-red-100 text-red-700",
  cancelled: "bg-yellow-100 text-yellow-700",
  escalated: "bg-orange-100 text-orange-700",
}

export function TaskDetailSheet({
  task,
  open,
  onOpenChange,
  onCancel,
}: {
  task: ReefTask | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onCancel?: (id: string) => void
}) {
  if (!task) return null

  const style = STATUS_STYLES[task.status] ?? "bg-gray-100 text-gray-700"

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-[400px] sm:w-[540px] overflow-y-auto">
        <SheetHeader>
          <SheetTitle className="flex items-center gap-2">
            <span className="truncate">{task.instruction}</span>
            <Badge className={cn("ml-2 capitalize", style)} variant="outline">
              {task.status}
            </Badge>
          </SheetTitle>
          <SheetDescription className="font-mono text-xs">{task.id}</SheetDescription>
        </SheetHeader>

        <div className="mt-6 space-y-4">
          <Field label="Role" value={task.required_role} />
          <Field label="Priority" value={String(task.priority)} />
          <Field label="Assigned Client" value={task.assigned_client || "—"} />
          <Field label="Max Retries" value={String(task.max_retries)} />
          <Field label="Timeout" value={task.timeout_ms ? `${task.timeout_ms}ms` : "—"} />
          <Field label="Attempts" value={String(task.attempt_count)} />
          <Field label="Escalations" value={String(task.escalation_count)} />
          <Field label="Created" value={task.created_at ? new Date(task.created_at).toLocaleString() : "—"} />
          <Field label="Updated" value={task.updated_at ? new Date(task.updated_at).toLocaleString() : "—"} />

          {task.required_skills && task.required_skills.length > 0 && (
            <Field label="Skills" value={task.required_skills.join(", ")} />
          )}

          {task.reply_to && (
            <Field label="Reply To" value={`${task.reply_to.channel} / ${task.reply_to.chat_id}`} />
          )}

          {task.parent_task_id && (
            <Field label="Parent Task" value={task.parent_task_id} />
          )}
          <Field label="Child Tasks" value={String(task.child_count)} />
          <Field label="Dependencies" value={String(task.dependency_count)} />
        </div>

        {task.status === "running" && onCancel && (
          <div className="mt-6">
            <Button
              variant="destructive"
              onClick={() => onCancel(task.id)}
              className="w-full"
            >
              <IconX className="size-4 mr-1" /> Cancel Task
            </Button>
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <span className="text-sm">{value}</span>
    </div>
  )
}

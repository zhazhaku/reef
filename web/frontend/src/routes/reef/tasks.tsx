import { IconRefresh, IconSearch } from "@tabler/icons-react"
import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import {
  type ReefTask,
  type ReefTaskFilter,
  cancelReefTask,
  fetchReefTasks,
} from "@/api/reef"
import { PageHeader } from "@/components/page-header"
import { TaskDetailSheet } from "@/components/reef/task-detail-sheet"
import { TaskTable } from "@/components/reef/task-table"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/reef/tasks")({
  component: ReefTasksPage,
})

const STATUS_OPTIONS = [
  { value: "", label: "reef.filterAll" },
  { value: "queued", label: "reef.statusQueued" },
  { value: "running", label: "reef.statusRunning" },
  { value: "completed", label: "reef.statusCompleted" },
  { value: "failed", label: "reef.statusFailed" },
  { value: "cancelled", label: "reef.statusCancelled" },
]

const ROLE_OPTIONS = [
  { value: "", label: "reef.filterAll" },
  { value: "coordinator", label: "reef.roleCoordinator" },
  { value: "executor", label: "reef.roleExecutor" },
  { value: "full", label: "reef.roleFull" },
]

function ReefTasksPage() {
  const { t } = useTranslation()
  const [tasks, setTasks] = useState<ReefTask[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [statusFilter, setStatusFilter] = useState("")
  const [roleFilter, setRoleFilter] = useState("")
  const [search, setSearch] = useState("")
  const [page, setPage] = useState(0)
  const [selectedTask, setSelectedTask] = useState<ReefTask | null>(null)
  const [sheetOpen, setSheetOpen] = useState(false)

  const limit = 20

  const loadTasks = useCallback(async () => {
    setLoading(true)
    setError("")
    try {
      const filter: ReefTaskFilter = { limit, offset: page * limit }
      if (statusFilter) filter.status = statusFilter
      if (roleFilter) filter.role = roleFilter
      if (search) filter.search = search
      const res = await fetchReefTasks(filter)
      setTasks(res.tasks)
      setTotal(res.total)
    } catch (e) {
      setError(e instanceof Error ? e.message : t("reef.loadError"))
    } finally {
      setLoading(false)
    }
  }, [statusFilter, roleFilter, search, page, t])

  useEffect(() => {
    loadTasks()
  }, [loadTasks])

  const handleSelect = (task: ReefTask) => {
    setSelectedTask(task)
    setSheetOpen(true)
  }

  const handleCancel = async (id: string) => {
    try {
      await cancelReefTask(id)
      loadTasks()
    } catch {
      // ignore
    }
  }

  const totalPages = Math.ceil(total / limit)

  return (
    <div className="flex h-full flex-col">
      <PageHeader title={t("reef.tasks")}>
        <Button
          variant="outline"
          size="sm"
          onClick={loadTasks}
          disabled={loading}
        >
          <IconRefresh className="size-4" />
          {t("reef.refresh")}
        </Button>
      </PageHeader>

      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4 pt-2 pb-8 sm:px-6">
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-[200px] max-w-sm flex-1">
            <IconSearch className="text-muted-foreground absolute left-2.5 top-1/2 size-4 -translate-y-1/2" />
            <Input
              placeholder={t("reef.searchTasks")}
              className="pl-8"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value)
                setPage(0)
              }}
            />
          </div>
          <Select
            value={statusFilter}
            onValueChange={(v) => {
              setStatusFilter(v)
              setPage(0)
            }}
          >
            <SelectTrigger className="w-[140px]">
              <SelectValue placeholder={t("reef.statusQueued")} />
            </SelectTrigger>
            <SelectContent>
              {STATUS_OPTIONS.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {t(o.label)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={roleFilter}
            onValueChange={(v) => {
              setRoleFilter(v)
              setPage(0)
            }}
          >
            <SelectTrigger className="w-[140px]">
              <SelectValue placeholder={t("reef.roleCoordinator")} />
            </SelectTrigger>
            <SelectContent>
              {ROLE_OPTIONS.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {t(o.label)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {loading && <Skeleton className="h-96 rounded-xl" />}

        {error && (
          <div className="bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm">
            {error}
          </div>
        )}

        {!loading && !error && (
          <>
            <TaskTable tasks={tasks} onSelect={handleSelect} />

            {totalPages > 1 && (
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground text-sm">
                  {t("reef.showingTasks", {
                    from: page * limit + 1,
                    to: Math.min((page + 1) * limit, total),
                    total,
                  })}
                </span>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={page === 0}
                    onClick={() => setPage((p) => p - 1)}
                  >
                    {t("reef.prev")}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={page >= totalPages - 1}
                    onClick={() => setPage((p) => p + 1)}
                  >
                    {t("reef.next")}
                  </Button>
                </div>
              </div>
            )}
          </>
        )}
      </div>

      <TaskDetailSheet
        task={selectedTask}
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        onCancel={handleCancel}
      />
    </div>
  )
}

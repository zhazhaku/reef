import { IconRefresh } from "@tabler/icons-react"
import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import {
  type ReefStatus,
  type ReefTask,
  fetchReefStatus,
  fetchReefTasks,
} from "@/api/reef"
import { PageHeader } from "@/components/page-header"
import { OverviewCards } from "@/components/reef/overview-cards"
import { TaskTable } from "@/components/reef/task-table"
import { TaskDetailSheet } from "@/components/reef/task-detail-sheet"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/reef/overview")({
  component: ReefOverviewPage,
})

function ReefOverviewPage() {
  const { t } = useTranslation()
  const [status, setStatus] = useState<ReefStatus | null>(null)
  const [tasks, setTasks] = useState<ReefTask[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [selectedTask, setSelectedTask] = useState<ReefTask | null>(null)
  const [sheetOpen, setSheetOpen] = useState(false)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError("")
    try {
      const [s, tasksRes] = await Promise.all([
        fetchReefStatus(),
        fetchReefTasks({ limit: 10 }),
      ])
      setStatus(s)
      setTasks(tasksRes.tasks)
    } catch (e) {
      setError(e instanceof Error ? e.message : t("reef.loadError"))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  return (
    <div className="flex h-full flex-col">
      <PageHeader title={t("reef.overview")}>
        <Button
          variant="outline"
          size="sm"
          onClick={fetchData}
          disabled={loading}
        >
          <IconRefresh className="size-4" />
          {t("reef.refresh")}
        </Button>
      </PageHeader>

      <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-4 pt-2 pb-8 sm:px-6">
        {loading && <OverviewSkeleton />}

        {error && (
          <div className="bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm">
            {error}
          </div>
        )}

        {!loading && !error && status && (
          <>
            <OverviewCards status={status} />

            <div className="text-muted-foreground flex gap-4 text-xs">
              <span>
                {t("reef.serverVersion")}: {status.server_version}
              </span>
              <span>
                {t("reef.uptime")}: {status.uptime}
              </span>
            </div>

            <div>
              <h3 className="text-foreground/90 mb-3 text-sm font-medium">
                {t("reef.recentTasks")}
              </h3>
              {tasks.length === 0 ? (
                <p className="text-muted-foreground text-sm">
                  {t("reef.noTasks")}
                </p>
              ) : (
                <TaskTable
                  tasks={tasks}
                  onSelect={(t) => {
                    setSelectedTask(t)
                    setSheetOpen(true)
                  }}
                />
              )}
            </div>
          </>
        )}
      </div>

      <TaskDetailSheet
        task={selectedTask}
        open={sheetOpen}
        onOpenChange={setSheetOpen}
      />
    </div>
  )
}

function OverviewSkeleton() {
  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-5">
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-24 rounded-xl" />
        ))}
      </div>
      <div className="space-y-3">
        <Skeleton className="h-4 w-32" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    </div>
  )
}

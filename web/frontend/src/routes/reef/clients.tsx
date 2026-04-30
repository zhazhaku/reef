import { IconRefresh } from "@tabler/icons-react"
import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { type ReefClient, fetchReefClients } from "@/api/reef"
import { PageHeader } from "@/components/page-header"
import { ClientTable } from "@/components/reef/client-table"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/reef/clients")({
  component: ReefClientsPage,
})

function ReefClientsPage() {
  const { t } = useTranslation()
  const [clients, setClients] = useState<ReefClient[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")

  const load = useCallback(async () => {
    setLoading(true)
    setError("")
    try {
      const res = await fetchReefClients()
      setClients(res.clients)
    } catch (e) {
      setError(e instanceof Error ? e.message : t("reef.loadError"))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    load()
  }, [load])

  return (
    <div className="flex h-full flex-col">
      <PageHeader title={t("reef.clients")}>
        <Button
          variant="outline"
          size="sm"
          onClick={load}
          disabled={loading}
        >
          <IconRefresh className="size-4" />
          {t("reef.refresh")}
        </Button>
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 pt-2 pb-8 sm:px-6">
        {loading && <Skeleton className="h-64 rounded-xl" />}

        {error && (
          <div className="bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm">
            {error}
          </div>
        )}

        {!loading && !error && (
          <>
            {clients.length === 0 ? (
              <div className="text-muted-foreground py-20 text-center text-sm">
                {t("reef.noClients")}
              </div>
            ) : (
              <ClientTable clients={clients} />
            )}
          </>
        )}
      </div>
    </div>
  )
}

import { useEffect, useRef, useState, useCallback } from "react"

export function useReefEvents(onEvent: (type: string, data: unknown) => void) {
  const [connected, setConnected] = useState(false)
  const eventSourceRef = useRef<EventSource | null>(null)
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  const connect = useCallback(() => {
    const es = new EventSource("/api/reef/events")
    eventSourceRef.current = es

    es.addEventListener("connected", () => {
      setConnected(true)
    })

    es.addEventListener("message", (e) => {
      try {
        const parsed = JSON.parse(e.data)
        onEventRef.current(parsed.type, parsed.data)
      } catch {
        // ignore parse errors
      }
    })

    es.addEventListener("stats_update", (e) => {
      try {
        onEventRef.current("stats_update", JSON.parse(e.data))
      } catch {}
    })
    es.addEventListener("task_created", (e) => {
      try {
        onEventRef.current("task_created", JSON.parse(e.data))
      } catch {}
    })
    es.addEventListener("task_completed", (e) => {
      try {
        onEventRef.current("task_completed", JSON.parse(e.data))
      } catch {}
    })
    es.addEventListener("task_failed", (e) => {
      try {
        onEventRef.current("task_failed", JSON.parse(e.data))
      } catch {}
    })
    es.addEventListener("client_connected", (e) => {
      try {
        onEventRef.current("client_connected", JSON.parse(e.data))
      } catch {}
    })
    es.addEventListener("client_disconnected", (e) => {
      try {
        onEventRef.current("client_disconnected", JSON.parse(e.data))
      } catch {}
    })

    es.onerror = () => {
      setConnected(false)
      es.close()
      eventSourceRef.current = null
      // Auto-reconnect after 3s
      setTimeout(connect, 3000)
    }
  }, [])

  useEffect(() => {
    connect()
    return () => {
      eventSourceRef.current?.close()
    }
  }, [connect])

  return { connected }
}

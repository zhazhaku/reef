// Reef API client for swarm orchestration.
import { launcherFetch } from "@/api/http"

export interface ReefStatus {
  server_version: string
  uptime: string
  connected_clients: number
  queued_tasks: number
  running_tasks: number
  completed_tasks: number
  failed_tasks: number
  total_tasks: number
  started_at: string
}

export interface ReefTask {
  id: string
  instruction: string
  status: string
  required_role: string
  required_skills?: string[]
  priority: number
  assigned_client?: string
  created_at: string
  updated_at: string
  timeout_ms: number
  max_retries: number
  attempt_count: number
  escalation_count: number
  parent_task_id?: string
  child_count: number
  dependency_count: number
  reply_to?: ReefReplyTo
}

export interface ReefReplyTo {
  channel: string
  chat_id: string
  user_id: string
  message_id?: string
  thread_id?: string
}

export interface ReefClient {
  id: string
  role: string
  skills: string[]
  state: string
  current_load: number
  capacity: number
  last_seen_at: string
}

export interface ReefTasksResponse {
  tasks: ReefTask[]
  total: number
}

export interface ReefClientsResponse {
  clients: ReefClient[]
}

export interface ReefTaskFilter {
  status?: string
  role?: string
  search?: string
  limit?: number
  offset?: number
}

const BASE = "/api/reef"

export async function fetchReefStatus(): Promise<ReefStatus> {
  const res = await launcherFetch(BASE + "/status")
  return res.json()
}

export async function fetchReefTasks(filter: ReefTaskFilter = {}): Promise<ReefTasksResponse> {
  const params = new URLSearchParams()
  if (filter.status) params.set("status", filter.status)
  if (filter.role) params.set("role", filter.role)
  if (filter.search) params.set("search", filter.search)
  if (filter.limit) params.set("limit", String(filter.limit))
  if (filter.offset) params.set("offset", String(filter.offset))
  const qs = params.toString()
  const res = await launcherFetch(BASE + "/tasks" + (qs ? "?" + qs : ""))
  return res.json()
}

export async function fetchReefTask(id: string): Promise<ReefTask> {
  const res = await launcherFetch(BASE + "/tasks/" + encodeURIComponent(id))
  return res.json()
}

export async function fetchReefSubTasks(id: string): Promise<ReefTasksResponse> {
  const res = await launcherFetch(BASE + "/tasks/" + encodeURIComponent(id) + "/subtasks")
  return res.json()
}

export async function cancelReefTask(id: string): Promise<void> {
  await launcherFetch(BASE + "/tasks/" + encodeURIComponent(id) + "/cancel", { method: "POST" })
}

export async function fetchReefClients(): Promise<ReefClientsResponse> {
  const res = await launcherFetch(BASE + "/clients")
  return res.json()
}

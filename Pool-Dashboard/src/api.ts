const tokenKey = 'pool-dashboard.admin-token'

interface Envelope<T> {
  code: number
  message: string
  data?: T
}

let onUnauthorized: (() => void) | undefined
let handlingUnauthorized = false

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
  }
}

export function getAdminToken(): string {
  return sessionStorage.getItem(tokenKey) ?? ''
}

export function setAdminToken(token: string): void {
  sessionStorage.setItem(tokenKey, token)
}

export function clearAdminToken(): void {
  sessionStorage.removeItem(tokenKey)
}

// 令牌校验复用受保护的账号列表接口，避免为了前端登录态新增一个只返回布尔值的后端接口
export async function verifyAdminToken(token: string): Promise<void> {
  const normalized = token.trim()
  if (normalized === '') {
    throw new ApiError('请输入 ADMIN_TOKEN', 400)
  }
  await send<unknown>('/api/v1/admin/accounts', {}, normalized, false)
}

// 设置全局失效处理器，所有携带失效令牌的管理请求都回到同一个登录状态
export function setUnauthorizedHandler(handler?: () => void): void {
  onUnauthorized = handler
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  return send<T>(path, init, getAdminToken(), true)
}

async function send<T>(path: string, init: RequestInit, token: string, notifyUnauthorized: boolean): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')

  if (init.body !== undefined) {
    headers.set('Content-Type', 'application/json')
  }

  if (token !== '') {
    headers.set('X-Admin-Token', token)
  }

  let response: Response
  try {
    response = await fetch(path, { ...init, headers, credentials: 'same-origin' })
  } catch {
    throw new ApiError('无法连接服务，请确认 Go API 已启动', 0)
  }

  let body: Envelope<T>
  try {
    body = (await response.json()) as Envelope<T>
  } catch {
    throw new ApiError('服务返回了非 JSON 响应', response.status)
  }

  if (!response.ok || body.code !== 0) {
    if (notifyUnauthorized && response.status === 401) {
      expireSession()
    }

    const message = response.status === 404 && path.startsWith('/api/v1/admin/')
      ? '管理接口未启用，请在服务端配置 ADMIN_TOKEN 后重启服务'
      : body.message || `请求失败（HTTP ${response.status}）`
    throw new ApiError(message, response.status)
  }
  return body.data as T
}

function expireSession(): void {
  if (handlingUnauthorized) return

  handlingUnauthorized = true
  clearAdminToken()
  onUnauthorized?.()
  queueMicrotask(() => {
    handlingUnauthorized = false
  })
}

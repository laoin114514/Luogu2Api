const tokenKey = 'pool-dashboard.admin-token'

interface Envelope<T> {
  code: number
  message: string
  data?: T
}

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

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')

  if (init.body !== undefined) {
    headers.set('Content-Type', 'application/json')
  }

  const token = getAdminToken()
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
    throw new ApiError(body.message || `请求失败（HTTP ${response.status}）`, response.status)
  }
  return body.data as T
}

export type AccountStatus = 'active' | 'new' | 'relogin_pending' | 'relogin_failed' | 'disabled' | 'banned'

export interface Account {
  id: number
  username: string
  luoguUid: number
  nickname: string
  online: boolean
  status: AccountStatus
  enabled: boolean
  failureCount: number
  lastError?: string
  nextVerifyAt?: string
  lastLoginAt?: string
  lastVerifiedAt?: string
  openSourceJoined: boolean
  name: string
  avatar: string
  isBanned: boolean
  ccfLevel: number
  xcpcLevel: number
  createdAt: string
}

export interface PoolStats {
  total: number
  online: number
  reloginPending: number
  reloginFailed: number
  disabled: number
  banned: number
  lastSweepAt?: string
}

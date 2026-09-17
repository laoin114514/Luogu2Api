<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { ApiError, clearAdminToken, getAdminToken, request, setAdminToken } from './api'

interface Account {
  id: number
  username: string
  luoguUid: number
  nickname: string
  online: boolean
  status: string
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

interface PoolStats {
  total: number
  online: number
  reloginPending: number
  reloginFailed: number
  disabled: number
  banned: number
  lastSweepAt?: string
}

const tokenDraft = ref(getAdminToken())
const hasToken = ref(tokenDraft.value !== '')
const accounts = ref<Account[]>([])
const stats = ref<PoolStats>()
const loading = ref(false)
const saving = ref(false)
const notice = ref('')
const error = ref('')
const query = ref('')
const statusFilter = ref('all')
const showTokenPanel = ref(!hasToken.value)
const showCreatePanel = ref(false)
const selected = ref<Account>()
const createForm = ref({ username: '', password: '', nickname: '' })

const filteredAccounts = computed(() => {
  const keyword = query.value.trim().toLocaleLowerCase()
  return accounts.value.filter((account) => {
    const matchesStatus = statusFilter.value === 'all' || account.status === statusFilter.value
    const matchesKeyword = keyword === '' || [account.username, account.nickname, account.name, String(account.luoguUid)]
      .join(' ')
      .toLocaleLowerCase()
      .includes(keyword)
    return matchesStatus && matchesKeyword
  })
})

const activeCount = computed(() => accounts.value.filter((account) => account.enabled && account.online).length)

function resetMessage(): void {
  notice.value = ''
  error.value = ''
}

function showError(cause: unknown): void {
  error.value = cause instanceof ApiError ? cause.message : '操作未完成，请稍后再试'
}

async function refresh(): Promise<void> {
  if (!hasToken.value) {
    showTokenPanel.value = true
    return
  }

  resetMessage()
  loading.value = true
  try {
    const [list, snapshot] = await Promise.all([
      request<Account[]>('/api/v1/admin/accounts'),
      request<PoolStats>('/api/v1/pool/status'),
    ])
    accounts.value = list
    stats.value = snapshot
    if (selected.value) {
      selected.value = list.find((account) => account.id === selected.value?.id)
    }
  } catch (cause) {
    showError(cause)
    if (cause instanceof ApiError && cause.status === 401) {
      clearAdminToken()
      hasToken.value = false
      tokenDraft.value = ''
      showTokenPanel.value = true
    }
  } finally {
    loading.value = false
  }
}

async function saveToken(): Promise<void> {
  const token = tokenDraft.value.trim()
  if (token === '') {
    error.value = '请输入 ADMIN_TOKEN'
    return
  }
  setAdminToken(token)
  hasToken.value = true
  showTokenPanel.value = false
  await refresh()
}

function forgetToken(): void {
  clearAdminToken()
  tokenDraft.value = ''
  hasToken.value = false
  accounts.value = []
  stats.value = undefined
  selected.value = undefined
  showTokenPanel.value = true
  notice.value = '已清除当前标签页中的管理令牌'
}

async function createAccount(): Promise<void> {
  if (createForm.value.username.trim() === '' || createForm.value.password === '') {
    error.value = '用户名和密码不能为空'
    return
  }
  resetMessage()
  saving.value = true
  try {
    const account = await request<Account>('/api/v1/admin/accounts', {
      method: 'POST',
      body: JSON.stringify({
        username: createForm.value.username.trim(),
        password: createForm.value.password,
        nickname: createForm.value.nickname.trim(),
      }),
    })
    createForm.value = { username: '', password: '', nickname: '' }
    showCreatePanel.value = false
    selected.value = account
    notice.value = `已导入 ${account.username}，首次登录正在由号池处理`
    await refresh()
  } catch (cause) {
    showError(cause)
  } finally {
    saving.value = false
  }
}

async function setEnabled(account: Account, enabled: boolean): Promise<void> {
  resetMessage()
  saving.value = true
  try {
    await request<Account>(`/api/v1/admin/accounts/${account.id}`, {
      method: 'PATCH',
      body: JSON.stringify({ enabled }),
    })
    notice.value = enabled ? `已启用 ${account.username}` : `已停用 ${account.username}`
    await refresh()
  } catch (cause) {
    showError(cause)
  } finally {
    saving.value = false
  }
}

async function relogin(account: Account): Promise<void> {
  resetMessage()
  saving.value = true
  try {
    await request<Account>(`/api/v1/admin/accounts/${account.id}/relogin`, { method: 'POST' })
    notice.value = `已请求 ${account.username} 立即重登`
    await refresh()
  } catch (cause) {
    showError(cause)
  } finally {
    saving.value = false
  }
}

async function deleteAccount(account: Account): Promise<void> {
  if (!window.confirm(`确认从号池删除「${account.username}」吗？这会软删除账号并立刻移出号池。`)) {
    return
  }
  resetMessage()
  saving.value = true
  try {
    await request(`/api/v1/admin/accounts/${account.id}`, { method: 'DELETE' })
    if (selected.value?.id === account.id) {
      selected.value = undefined
    }
    notice.value = `已从号池删除 ${account.username}`
    await refresh()
  } catch (cause) {
    showError(cause)
  } finally {
    saving.value = false
  }
}

function statusLabel(status: string): string {
  return {
    active: '可用',
    new: '待登录',
    relogin_pending: '待重登',
    relogin_failed: '重登失败',
    disabled: '已停用',
    banned: '已封禁',
  }[status] ?? status
}

function formatTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? '—' : new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
  }).format(date)
}

onMounted(refresh)
</script>

<template>
  <main class="dashboard-shell">
    <aside class="sidebar">
      <a class="brand" href="/dashboard/" aria-label="Pool Dashboard 首页">
        <span class="brand-mark">P</span>
        <span>Pool<br /><strong>Dashboard</strong></span>
      </a>

      <div class="side-section">
        <span class="side-label">号池概览</span>
        <div class="availability-card">
          <span>当前可调度</span>
          <strong>{{ activeCount }}</strong>
          <small>共 {{ stats?.total ?? 0 }} 个账号</small>
        </div>
      </div>

      <div class="side-section legend">
        <span class="side-label">状态说明</span>
        <p><i class="dot active"></i>可用：可被业务请求选中</p>
        <p><i class="dot pending"></i>待处理：等待验证或重登</p>
        <p><i class="dot danger"></i>不可用：需要人工关注</p>
      </div>

      <div class="sidebar-bottom">
        <button class="text-button" type="button" @click="showTokenPanel = true">管理令牌</button>
        <button class="text-button muted" type="button" @click="forgetToken">清除令牌</button>
      </div>
    </aside>

    <section class="content">
      <header class="topbar">
        <div>
          <p class="eyebrow">LUOGU2API · ACCOUNT POOL</p>
          <h1>账号控制台</h1>
          <p class="subtitle">查看号池状态，快速导入、启停与维护账号。</p>
        </div>
        <div class="top-actions">
          <button class="icon-button" type="button" title="刷新数据" :disabled="loading || saving" @click="refresh">↻</button>
          <button class="primary-button" type="button" :disabled="!hasToken" @click="showCreatePanel = true">
            <span>＋</span> 导入账号
          </button>
        </div>
      </header>

      <p v-if="notice" class="message success">{{ notice }}</p>
      <p v-if="error" class="message error">{{ error }}</p>

      <section class="metric-grid" aria-label="号池状态">
        <article class="metric-card accent-blue"><span>在线可用</span><strong>{{ stats?.online ?? 0 }}</strong><small>active sessions</small></article>
        <article class="metric-card accent-amber"><span>等待恢复</span><strong>{{ stats?.reloginPending ?? 0 }}</strong><small>new / pending</small></article>
        <article class="metric-card accent-red"><span>重登失败</span><strong>{{ stats?.reloginFailed ?? 0 }}</strong><small>需检查凭据或 OCR</small></article>
        <article class="metric-card accent-slate"><span>停用 / 封禁</span><strong>{{ (stats?.disabled ?? 0) + (stats?.banned ?? 0) }}</strong><small>不参与调度</small></article>
      </section>

      <section class="accounts-panel">
        <div class="panel-heading">
          <div>
            <h2>账号列表</h2>
            <p>最后扫描：{{ formatTime(stats?.lastSweepAt) }}</p>
          </div>
          <div class="filters">
            <label class="search"><span>⌕</span><input v-model="query" type="search" placeholder="搜索账号、昵称或 UID" /></label>
            <select v-model="statusFilter" aria-label="按状态筛选">
              <option value="all">全部状态</option>
              <option value="active">可用</option>
              <option value="new">待登录</option>
              <option value="relogin_pending">待重登</option>
              <option value="relogin_failed">重登失败</option>
              <option value="disabled">已停用</option>
              <option value="banned">已封禁</option>
            </select>
          </div>
        </div>

        <div v-if="loading" class="empty-state">正在同步号池数据…</div>
        <div v-else-if="!hasToken" class="empty-state">请输入管理令牌后读取账号信息。</div>
        <div v-else-if="filteredAccounts.length === 0" class="empty-state">没有匹配的账号。可以直接导入一个新账号。</div>
        <div v-else class="table-wrap">
          <table>
            <thead><tr><th>账号</th><th>运行状态</th><th>号池</th><th>最近验证</th><th>失败信息</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-for="account in filteredAccounts" :key="account.id" :class="{ selected: selected?.id === account.id }">
                <td>
                  <button class="account-cell" type="button" @click="selected = account">
                    <img v-if="account.avatar" :src="account.avatar" alt="" />
                    <span v-else class="avatar-fallback">{{ (account.nickname || account.username).slice(0, 1).toUpperCase() }}</span>
                    <span><strong>{{ account.nickname || account.name || account.username }}</strong><small>@{{ account.username }} · {{ account.luoguUid || 'UID 未知' }}</small></span>
                  </button>
                </td>
                <td><span class="status-pill" :class="account.status">{{ statusLabel(account.status) }}</span></td>
                <td><span :class="['switch-state', account.enabled && account.online ? 'online' : 'offline']"><i></i>{{ account.enabled ? (account.online ? '已调度' : '未就绪') : '已停用' }}</span></td>
                <td>{{ formatTime(account.lastVerifiedAt || account.lastLoginAt) }}</td>
                <td class="error-cell" :title="account.lastError">{{ account.lastError || '—' }}</td>
                <td>
                  <div class="row-actions">
                    <button type="button" :disabled="saving" @click="relogin(account)">重登</button>
                    <button type="button" :disabled="saving" @click="setEnabled(account, !account.enabled)">{{ account.enabled ? '停用' : '启用' }}</button>
                    <button class="danger-button" type="button" :disabled="saving" @click="deleteAccount(account)">删除</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </section>

    <aside v-if="selected" class="detail-panel">
      <button class="close-button" type="button" aria-label="关闭详情" @click="selected = undefined">×</button>
      <div class="profile-heading">
        <img v-if="selected.avatar" :src="selected.avatar" alt="" />
        <span v-else class="profile-avatar">{{ (selected.nickname || selected.username).slice(0, 1).toUpperCase() }}</span>
        <div><p>账号详情</p><h2>{{ selected.nickname || selected.name || selected.username }}</h2><span>@{{ selected.username }}</span></div>
      </div>
      <span class="status-pill" :class="selected.status">{{ statusLabel(selected.status) }}</span>
      <dl class="detail-list">
        <div><dt>洛谷 UID</dt><dd>{{ selected.luoguUid || '—' }}</dd></div>
        <div><dt>人工开关</dt><dd>{{ selected.enabled ? '已启用' : '已停用' }}</dd></div>
        <div><dt>连续失败</dt><dd>{{ selected.failureCount }} 次</dd></div>
        <div><dt>下次验证</dt><dd>{{ formatTime(selected.nextVerifyAt) }}</dd></div>
        <div><dt>创建时间</dt><dd>{{ formatTime(selected.createdAt) }}</dd></div>
        <div><dt>公开计划</dt><dd>{{ selected.openSourceJoined ? '已加入' : '未加入' }}</dd></div>
      </dl>
      <p v-if="selected.lastError" class="detail-error"><strong>最近错误</strong>{{ selected.lastError }}</p>
    </aside>

    <div v-if="showTokenPanel || showCreatePanel" class="modal-backdrop" @click.self="showTokenPanel = false; showCreatePanel = false">
      <form v-if="showTokenPanel" class="modal" @submit.prevent="saveToken">
        <p class="eyebrow">SECURE ACCESS</p>
        <h2>输入管理令牌</h2>
        <p>使用服务端配置的 <code>ADMIN_TOKEN</code>。令牌仅保存在当前浏览器标签页，关闭标签页即清除。</p>
        <label>ADMIN_TOKEN<input v-model="tokenDraft" type="password" autocomplete="off" autofocus /></label>
        <div class="modal-actions"><button class="secondary-button" type="button" @click="showTokenPanel = false">取消</button><button class="primary-button" type="submit">连接号池</button></div>
      </form>
      <form v-else class="modal" @submit.prevent="createAccount">
        <p class="eyebrow">NEW ACCOUNT</p>
        <h2>导入号池账号</h2>
        <p>提交后会写入账号池并尝试首次登录。登录异常会由扫描器后续重试。</p>
        <label>洛谷用户名<input v-model="createForm.username" maxlength="64" autocomplete="username" required /></label>
        <label>登录密码<input v-model="createForm.password" type="password" autocomplete="new-password" required /></label>
        <label>显示昵称（可选）<input v-model="createForm.nickname" maxlength="64" /></label>
        <div class="modal-actions"><button class="secondary-button" type="button" @click="showCreatePanel = false">取消</button><button class="primary-button" :disabled="saving" type="submit">{{ saving ? '正在导入…' : '确认导入' }}</button></div>
      </form>
    </div>
  </main>
</template>

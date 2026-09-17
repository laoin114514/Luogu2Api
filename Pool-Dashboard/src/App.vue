<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  CircleCheckFilled,
  Connection,
  Key,
  Loading,
  Lock,
  Plus,
  Refresh,
  SwitchButton,
  Timer,
  UserFilled,
  WarningFilled,
} from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'

import {
  ApiError,
  clearAdminToken,
  getAdminToken,
  request,
  setAdminToken,
  setUnauthorizedHandler,
  verifyAdminToken,
} from './api'
import type { Account, AccountStatus, PoolStats } from './types'

const authChecking = ref(true)
const authenticated = ref(false)
const loginLoading = ref(false)
const dashboardLoading = ref(false)
const actionLoading = ref(false)
const loginError = ref('')
const sessionNotice = ref('')
const loginFormRef = ref<FormInstance>()
const accountFormRef = ref<FormInstance>()
const accountDialogOpen = ref(false)
const detailDrawerOpen = ref(false)
const selectedAccount = ref<Account>()
const accounts = ref<Account[]>([])
const stats = ref<PoolStats>()
const search = ref('')
const statusFilter = ref<'all' | AccountStatus>('all')

const loginForm = reactive({ token: '' })
const accountForm = reactive({ username: '', password: '', nickname: '' })

const loginRules: FormRules<typeof loginForm> = {
  token: [{ required: true, message: '请输入 ADMIN_TOKEN', trigger: 'blur' }],
}

const accountRules: FormRules<typeof accountForm> = {
  username: [
    { required: true, message: '请输入洛谷用户名', trigger: 'blur' },
    { max: 64, message: '用户名不能超过 64 个字符', trigger: 'blur' },
  ],
  password: [{ required: true, message: '请输入登录密码', trigger: 'blur' }],
  nickname: [{ max: 64, message: '显示昵称不能超过 64 个字符', trigger: 'blur' }],
}

const filteredAccounts = computed(() => {
  const keyword = search.value.trim().toLocaleLowerCase()
  return accounts.value.filter((account) => {
    const matchesStatus = statusFilter.value === 'all' || account.status === statusFilter.value
    const matchesKeyword = keyword === '' || [account.username, account.nickname, account.name, String(account.luoguUid)]
      .join(' ')
      .toLocaleLowerCase()
      .includes(keyword)
    return matchesStatus && matchesKeyword
  })
})

const poolSegments = computed(() => {
  const total = stats.value?.total ?? 0
  const ratio = (value: number) => total === 0 ? 0 : Math.round((value / total) * 100)
  return [
    { label: '在线可用', value: stats.value?.online ?? 0, percentage: ratio(stats.value?.online ?? 0), color: '#67c23a' },
    { label: '等待恢复', value: stats.value?.reloginPending ?? 0, percentage: ratio(stats.value?.reloginPending ?? 0), color: '#e6a23c' },
    { label: '重登失败', value: stats.value?.reloginFailed ?? 0, percentage: ratio(stats.value?.reloginFailed ?? 0), color: '#f56c6c' },
    { label: '停用或封禁', value: (stats.value?.disabled ?? 0) + (stats.value?.banned ?? 0), percentage: ratio((stats.value?.disabled ?? 0) + (stats.value?.banned ?? 0)), color: '#909399' },
  ]
})

async function bootstrap(): Promise<void> {
  setUnauthorizedHandler(() => endSession('管理令牌无效或已失效，请重新登录'))

  if (getAdminToken() === '') {
    authChecking.value = false
    return
  }

  authenticated.value = true
  try {
    await loadDashboard()
  } catch {
    // loadDashboard 已处理错误；401 会通过全局失效处理器回到登录页
  } finally {
    authChecking.value = false
  }
}

async function login(): Promise<void> {
  const valid = await loginFormRef.value?.validate().catch(() => false)
  if (!valid) return

  loginError.value = ''
  sessionNotice.value = ''
  loginLoading.value = true
  try {
    // 只有后端实际接受令牌后才写入 sessionStorage，避免保留错误令牌
    await verifyAdminToken(loginForm.token)
    setAdminToken(loginForm.token.trim())
    loginForm.token = ''
    authenticated.value = true
    await loadDashboard()
    ElMessage.success('已连接到号池管理接口')
  } catch (cause) {
    loginError.value = messageOf(cause)
  } finally {
    loginLoading.value = false
  }
}

function logout(): void {
  endSession('已退出管理台')
}

function endSession(message: string): void {
  clearAdminToken()
  authenticated.value = false
  accounts.value = []
  stats.value = undefined
  selectedAccount.value = undefined
  detailDrawerOpen.value = false
  loginForm.token = ''
  loginError.value = ''
  sessionNotice.value = message
  if (!authChecking.value) {
    ElMessage.info(message)
  }
}

async function loadDashboard(): Promise<void> {
  dashboardLoading.value = true
  try {
    const [accountList, snapshot] = await Promise.all([
      request<Account[]>('/api/v1/admin/accounts'),
      request<PoolStats>('/api/v1/pool/status'),
    ])
    accounts.value = accountList
    stats.value = snapshot
    if (selectedAccount.value) {
      selectedAccount.value = accountList.find((account) => account.id === selectedAccount.value?.id)
    }
  } catch (cause) {
    if (!(cause instanceof ApiError && cause.status === 401)) {
      ElMessage.error(messageOf(cause))
    }
    throw cause
  } finally {
    dashboardLoading.value = false
  }
}

async function createAccount(): Promise<void> {
  const valid = await accountFormRef.value?.validate().catch(() => false)
  if (!valid) return

  actionLoading.value = true
  try {
    const account = await request<Account>('/api/v1/admin/accounts', {
      method: 'POST',
      body: JSON.stringify({
        username: accountForm.username.trim(),
        password: accountForm.password,
        nickname: accountForm.nickname.trim(),
      }),
    })
    accountDialogOpen.value = false
    accountFormRef.value?.resetFields()
    selectedAccount.value = account
    detailDrawerOpen.value = true
    ElMessage.success(`已导入 ${account.username}，号池将尝试首次登录`)
    await loadDashboard()
  } catch (cause) {
    showRequestError(cause)
  } finally {
    actionLoading.value = false
  }
}

async function setEnabled(account: Account, enabled: boolean): Promise<void> {
  await runAction(async () => {
    await request<Account>(`/api/v1/admin/accounts/${account.id}`, {
      method: 'PATCH',
      body: JSON.stringify({ enabled }),
    })
  }, enabled ? `已启用 ${account.username}` : `已停用 ${account.username}`)
}

async function relogin(account: Account): Promise<void> {
  await runAction(
    () => request<Account>(`/api/v1/admin/accounts/${account.id}/relogin`, { method: 'POST' }),
    `已请求 ${account.username} 立即重登`,
  )
}

async function deleteAccount(account: Account): Promise<void> {
  try {
    await ElMessageBox.confirm(
      `删除后会将 ${account.username} 软删除并立即移出号池。再次用同名账号导入会复活该行。`,
      '确认删除账号？',
      { confirmButtonText: '确认删除', cancelButtonText: '取消', type: 'warning' },
    )
  } catch {
    return
  }

  await runAction(
    () => request(`/api/v1/admin/accounts/${account.id}`, { method: 'DELETE' }),
    `已从号池删除 ${account.username}`,
  )
}

async function runAction(action: () => Promise<unknown>, success: string): Promise<void> {
  actionLoading.value = true
  try {
    await action()
    ElMessage.success(success)
    await loadDashboard()
  } catch (cause) {
    showRequestError(cause)
  } finally {
    actionLoading.value = false
  }
}

function openAccountDialog(): void {
  accountFormRef.value?.resetFields()
  accountDialogOpen.value = true
}

function openDetail(account: Account): void {
  selectedAccount.value = account
  detailDrawerOpen.value = true
}

function showRequestError(cause: unknown): void {
  if (!(cause instanceof ApiError && cause.status === 401)) {
    ElMessage.error(messageOf(cause))
  }
}

function messageOf(cause: unknown): string {
  return cause instanceof ApiError ? cause.message : '操作未完成，请稍后再试'
}

function statusLabel(status: AccountStatus): string {
  return {
    active: '可用',
    new: '待登录',
    relogin_pending: '待重登',
    relogin_failed: '重登失败',
    disabled: '已停用',
    banned: '已封禁',
  }[status]
}

function statusTagType(status: AccountStatus): 'success' | 'warning' | 'danger' | 'info' {
  return {
    active: 'success',
    new: 'warning',
    relogin_pending: 'warning',
    relogin_failed: 'danger',
    disabled: 'info',
    banned: 'danger',
  }[status]
}

function formatTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? '—' : new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
  }).format(date)
}

onMounted(bootstrap)
onBeforeUnmount(() => setUnauthorizedHandler())
</script>

<template>
  <el-config-provider>
    <main v-if="authChecking" class="boot-page">
      <el-icon class="is-loading" :size="28"><Loading /></el-icon>
      <span>正在校验管理会话…</span>
    </main>

    <main v-else-if="!authenticated" class="auth-page">
      <el-card class="auth-card" shadow="always">
        <template #header>
          <div class="auth-card__header">
            <el-icon :size="26"><Key /></el-icon>
            <div><strong>Pool Dashboard</strong><span>号池管理认证</span></div>
          </div>
        </template>

        <el-alert title="仅限管理员使用" description="令牌只保存在当前浏览器标签页中，关闭标签页后会自动清除" type="info" :closable="false" show-icon />
        <el-alert v-if="sessionNotice" class="auth-notice" :title="sessionNotice" type="warning" :closable="false" show-icon />
        <el-form ref="loginFormRef" class="auth-form" :model="loginForm" :rules="loginRules" label-position="top" @submit.prevent="login">
          <el-form-item label="ADMIN_TOKEN" prop="token">
            <el-input v-model="loginForm.token" type="password" autocomplete="off" show-password placeholder="输入服务端配置的管理令牌">
              <template #prefix><el-icon><Lock /></el-icon></template>
            </el-input>
          </el-form-item>
          <el-alert v-if="loginError" class="auth-error" :title="loginError" type="error" :closable="false" show-icon />
          <el-button class="auth-submit" type="primary" native-type="submit" :loading="loginLoading">验证并进入管理台</el-button>
        </el-form>
      </el-card>
    </main>

    <el-container v-else class="dashboard-layout">
      <el-aside width="230px" class="dashboard-aside">
        <div class="brand"><el-icon :size="25"><Connection /></el-icon><span>Pool Dashboard<small>LUOGU2API</small></span></div>
        <el-menu default-active="overview" class="dashboard-menu">
          <el-menu-item index="overview"><el-icon><Connection /></el-icon><span>号池看板</span></el-menu-item>
          <el-menu-item index="accounts"><el-icon><UserFilled /></el-icon><span>账号管理</span></el-menu-item>
        </el-menu>
        <div class="aside-footer">
          <el-tag type="success" effect="dark" round>管理会话已验证</el-tag>
          <el-button text type="info" :icon="SwitchButton" @click="logout">退出管理台</el-button>
        </div>
      </el-aside>

      <el-container>
        <el-header class="dashboard-header">
          <div><h1>号池看板</h1><p>监测账号可用性，并通过管理接口维护号池。</p></div>
          <div class="header-actions">
            <el-tooltip content="刷新号池快照"><el-button circle :icon="Refresh" :loading="dashboardLoading" @click="loadDashboard" /></el-tooltip>
            <el-button type="primary" :icon="Plus" @click="openAccountDialog">导入账号</el-button>
          </div>
        </el-header>

        <el-main class="dashboard-main">
          <el-row :gutter="16" class="stat-grid">
            <el-col v-for="segment in poolSegments" :key="segment.label" :xs="24" :sm="12" :lg="6">
              <el-card shadow="never" class="stat-card">
                <el-statistic :value="segment.value"><template #title><span>{{ segment.label }}</span></template></el-statistic>
                <el-progress :percentage="segment.percentage" :color="segment.color" :show-text="false" :stroke-width="6" />
                <small>占号池 {{ segment.percentage }}%</small>
              </el-card>
            </el-col>
          </el-row>

          <el-row :gutter="16" class="overview-row">
            <el-col :xs="24" :lg="15">
              <el-card shadow="never" class="overview-card">
                <template #header><div class="card-title"><span>运行概览</span><el-tag type="info" effect="plain">最后扫描：{{ formatTime(stats?.lastSweepAt) }}</el-tag></div></template>
                <el-descriptions :column="2" border>
                  <el-descriptions-item label="账号总数">{{ stats?.total ?? 0 }}</el-descriptions-item>
                  <el-descriptions-item label="当前在线"><el-tag type="success">{{ stats?.online ?? 0 }} 个可调度</el-tag></el-descriptions-item>
                  <el-descriptions-item label="待恢复">{{ stats?.reloginPending ?? 0 }} 个等待验证或重登</el-descriptions-item>
                  <el-descriptions-item label="人工处理">{{ (stats?.reloginFailed ?? 0) + (stats?.disabled ?? 0) + (stats?.banned ?? 0) }} 个需要关注</el-descriptions-item>
                </el-descriptions>
              </el-card>
            </el-col>
            <el-col :xs="24" :lg="9">
              <el-card shadow="never" class="overview-card state-guide">
                <template #header><span class="card-title">状态说明</span></template>
                <p><el-icon color="#67c23a"><CircleCheckFilled /></el-icon><span><strong>在线可用</strong>：可以被业务请求选中</span></p>
                <p><el-icon color="#e6a23c"><Timer /></el-icon><span><strong>等待恢复</strong>：扫描器将继续验证或重登</span></p>
                <p><el-icon color="#f56c6c"><WarningFilled /></el-icon><span><strong>需要处理</strong>：检查凭据、封禁状态或 OCR 环境</span></p>
              </el-card>
            </el-col>
          </el-row>

          <el-card shadow="never" class="account-card">
            <template #header>
              <div class="card-title account-card__title">
                <span>账号管理</span>
                <div class="account-filters">
                  <el-input v-model="search" clearable :prefix-icon="UserFilled" placeholder="搜索账号、昵称或 UID" />
                  <el-select v-model="statusFilter" aria-label="账号状态筛选">
                    <el-option label="全部状态" value="all" />
                    <el-option label="可用" value="active" /><el-option label="待登录" value="new" />
                    <el-option label="待重登" value="relogin_pending" /><el-option label="重登失败" value="relogin_failed" />
                    <el-option label="已停用" value="disabled" /><el-option label="已封禁" value="banned" />
                  </el-select>
                </div>
              </div>
            </template>

            <el-table v-loading="dashboardLoading" :data="filteredAccounts" row-key="id" :empty-text="search || statusFilter !== 'all' ? '没有匹配的账号' : '号池还没有账号，点击右上角导入'" @row-click="openDetail">
              <el-table-column label="账号" min-width="210">
                <template #default="{ row }: { row: Account }">
                  <div class="account-profile"><el-avatar :size="34" :src="row.avatar">{{ (row.nickname || row.username).slice(0, 1).toUpperCase() }}</el-avatar><div><strong>{{ row.nickname || row.name || row.username }}</strong><small>@{{ row.username }} · {{ row.luoguUid || 'UID 未知' }}</small></div></div>
                </template>
              </el-table-column>
              <el-table-column label="状态" width="118"><template #default="{ row }: { row: Account }"><el-tag :type="statusTagType(row.status)" effect="light">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
              <el-table-column label="调度" width="120"><template #default="{ row }: { row: Account }"><el-tag :type="row.enabled && row.online ? 'success' : 'info'" effect="plain">{{ row.enabled ? (row.online ? '已调度' : '未就绪') : '已停用' }}</el-tag></template></el-table-column>
              <el-table-column label="最近验证" width="170"><template #default="{ row }: { row: Account }">{{ formatTime(row.lastVerifiedAt || row.lastLoginAt) }}</template></el-table-column>
              <el-table-column label="失败信息" min-width="170" show-overflow-tooltip><template #default="{ row }: { row: Account }">{{ row.lastError || '—' }}</template></el-table-column>
              <el-table-column label="操作" width="216" fixed="right">
                <template #default="{ row }: { row: Account }"><el-button text type="primary" :loading="actionLoading" @click.stop="relogin(row)">重登</el-button><el-button text type="primary" :loading="actionLoading" @click.stop="setEnabled(row, !row.enabled)">{{ row.enabled ? '停用' : '启用' }}</el-button><el-button text type="danger" :loading="actionLoading" @click.stop="deleteAccount(row)">删除</el-button></template>
              </el-table-column>
            </el-table>
          </el-card>
        </el-main>
      </el-container>
    </el-container>

    <el-dialog v-model="accountDialogOpen" title="导入号池账号" width="440px" :close-on-click-modal="false">
      <el-alert title="账号写入后会立即尝试首次登录" description="网络或 OCR 抖动时会交由扫描器后续重试，不会在页面中返回密码或 cookie" type="info" :closable="false" show-icon />
      <el-form ref="accountFormRef" class="account-form" :model="accountForm" :rules="accountRules" label-position="top" @submit.prevent="createAccount">
        <el-form-item label="洛谷用户名" prop="username"><el-input v-model="accountForm.username" maxlength="64" autocomplete="username" /></el-form-item>
        <el-form-item label="登录密码" prop="password"><el-input v-model="accountForm.password" type="password" show-password autocomplete="new-password" /></el-form-item>
        <el-form-item label="显示昵称（可选）" prop="nickname"><el-input v-model="accountForm.nickname" maxlength="64" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="accountDialogOpen = false">取消</el-button><el-button type="primary" :loading="actionLoading" @click="createAccount">确认导入</el-button></template>
    </el-dialog>

    <el-drawer v-model="detailDrawerOpen" size="360px" title="账号详情">
      <template v-if="selectedAccount">
        <div class="drawer-profile"><el-avatar :size="54" :src="selectedAccount.avatar">{{ (selectedAccount.nickname || selectedAccount.username).slice(0, 1).toUpperCase() }}</el-avatar><div><h2>{{ selectedAccount.nickname || selectedAccount.name || selectedAccount.username }}</h2><p>@{{ selectedAccount.username }}</p><el-tag :type="statusTagType(selectedAccount.status)">{{ statusLabel(selectedAccount.status) }}</el-tag></div></div>
        <el-descriptions :column="1" border>
          <el-descriptions-item label="洛谷 UID">{{ selectedAccount.luoguUid || '—' }}</el-descriptions-item>
          <el-descriptions-item label="人工开关">{{ selectedAccount.enabled ? '已启用' : '已停用' }}</el-descriptions-item>
          <el-descriptions-item label="连续失败">{{ selectedAccount.failureCount }} 次</el-descriptions-item>
          <el-descriptions-item label="下次验证">{{ formatTime(selectedAccount.nextVerifyAt) }}</el-descriptions-item>
          <el-descriptions-item label="创建时间">{{ formatTime(selectedAccount.createdAt) }}</el-descriptions-item>
          <el-descriptions-item label="公开计划">{{ selectedAccount.openSourceJoined ? '已加入' : '未加入' }}</el-descriptions-item>
        </el-descriptions>
        <el-alert v-if="selectedAccount.lastError" class="drawer-error" title="最近错误" :description="selectedAccount.lastError" type="error" :closable="false" show-icon />
      </template>
    </el-drawer>
  </el-config-provider>
</template>

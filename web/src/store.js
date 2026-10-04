import { reactive } from 'vue'
import { api, getToken, setToken, clearToken } from './api.js'

export const store = reactive({
  user: null,
  users: [],
  enterprises: [],
  routes: [],
  loading: false,
  toast: '',
})

let toastTimer = null
export function notify(msg) {
  store.toast = msg
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => (store.toast = ''), 3200)
}

export async function bootstrap() {
  const data = await api.bootstrap()
  store.users = data.users
  store.enterprises = data.enterprises
  store.routes = data.routes
  const name = getToken()
  if (name) {
    store.user = store.users.find((u) => u.username === name) || null
  }
}

export async function login(username) {
  const data = await api.login(username)
  setToken(data.token)
  store.user = data.user
}

export function logout() {
  clearToken()
  store.user = null
}

export const ROLE_LABEL = {
  kitchen: '厨房',
  courier: '配送员',
  frontdesk: '企业前台',
  reviewer: '运营复核',
  admin: '调度管理',
}

export const STATUS_LABEL = {
  draft: '装箱中',
  sealed: '已封箱',
  assigned: '已指派',
  in_transit: '配送途中',
  delivered: '已送达',
  signed: '已签收',
  resolving: '差异处理中',
  closed: '已结清',
}

export const DIFF_LABEL = {
  short: '少盒',
  wrong: '错餐',
  broken: '封签破损',
  no_one: '无人签收',
  open: '待复核',
  approved: '已批准',
  rejected: '已驳回',
}

export const MEAL_LABEL = { breakfast: '早餐', lunch: '午餐', dinner: '晚餐' }

export function yuan(fen) {
  return `¥${(fen / 100).toFixed(2)}`
}

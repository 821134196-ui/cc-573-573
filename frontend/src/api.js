import { reactive } from 'vue'

const STORAGE_KEY = 'campusmeal_user'

export const session = reactive({
  user: JSON.parse(localStorage.getItem(STORAGE_KEY) || 'null'),
  set(u) {
    this.user = u
    localStorage.setItem(STORAGE_KEY, JSON.stringify(u))
  },
  clear() {
    this.user = null
    localStorage.removeItem(STORAGE_KEY)
  },
})

async function request(method, path, body) {
  const headers = { 'Content-Type': 'application/json' }
  if (session.user) headers['X-User-Id'] = session.user.id
  const res = await fetch('/api' + path, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `请求失败 ${res.status}`)
  return data
}

export const api = {
  get: (p) => request('GET', p),
  post: (p, b) => request('POST', p, b || {}),
}

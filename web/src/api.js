const BASE = '/api'

export function getToken() {
  return localStorage.getItem('mc_user') || ''
}
export function setToken(name) {
  localStorage.setItem('mc_user', name)
}
export function clearToken() {
  localStorage.removeItem('mc_user')
}

async function request(method, path, body) {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      'Content-Type': 'application/json',
      'X-User': getToken(),
    },
    body: body ? JSON.stringify(body) : undefined,
  })
  let payload
  try {
    payload = await res.json()
  } catch {
    payload = { ok: false, error: '服务器响应解析失败' }
  }
  if (!payload.ok) {
    const err = new Error(payload.error || `HTTP ${res.status}`)
    err.status = res.status
    throw err
  }
  return payload.data
}

export const api = {
  get: (p) => request('GET', p),
  post: (p, b) => request('POST', p, b || {}),
  put: (p, b) => request('PUT', p, b || {}),
  bootstrap: () => request('GET', '/bootstrap'),
  login: (username) => request('POST', '/login', { username }),
}

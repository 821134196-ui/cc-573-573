<template>
  <div v-if="!session.user" class="login-box panel">
    <h2>园区团餐配送批次与收餐核对系统</h2>
    <p class="muted">选择演示账号登录（本地模拟，无密码）</p>
    <div class="grid">
      <select v-model="name">
        <option value="" disabled>请选择用户…</option>
        <option v-for="u in users" :key="u.id" :value="u.name">
          {{ roleLabel[u.role] }} · {{ u.name }}
        </option>
      </select>
      <button @click="login">登录</button>
      <p v-if="err" class="err">{{ err }}</p>
    </div>
  </div>

  <template v-else>
    <header class="app-header">
      <h1>🍱 团餐批次核对</h1>
      <div class="who">
        <span>{{ roleLabel[session.user.role] }} · {{ session.user.name }}</span>
        <button class="gray" @click="logout">退出</button>
      </div>
    </header>
    <nav class="tabs">
      <button v-for="t in tabs" :key="t.key" :class="{ active: tab === t.key }" @click="tab = t.key">
        {{ t.label }}
      </button>
    </nav>
    <main class="page">
      <KitchenView v-if="tab === 'kitchen'" />
      <CourierView v-else-if="tab === 'courier'" />
      <FrontDeskView v-else-if="tab === 'frontdesk'" />
      <ReviewView v-else-if="tab === 'review'" />
      <AuditView v-if="tab === 'audit'" />
      <p v-else class="muted">当前角色没有专属工作台，可在「全局对账」中查看数量核对结果。</p>
    </main>
    <div v-if="flash.text" class="flash" :class="{ bad: flash.bad }">{{ flash.text }}</div>
  </template>
</template>

<script setup>
import { ref, reactive, computed, onMounted, provide } from 'vue'
import { session, api } from './api'
import { ROLE_LABEL as roleLabel } from './labels'
import KitchenView from './views/KitchenView.vue'
import CourierView from './views/CourierView.vue'
import FrontDeskView from './views/FrontDeskView.vue'
import ReviewView from './views/ReviewView.vue'
import AuditView from './views/AuditView.vue'

const users = ref([])
const name = ref('')
const err = ref('')
const tab = ref('')
const flash = reactive({ text: '', bad: false, timer: null })

provide('flash', (text, bad = false) => {
  flash.text = text; flash.bad = bad
  clearTimeout(flash.timer)
  flash.timer = setTimeout(() => { flash.text = '' }, 2600)
})

const ALL_TABS = [
  { key: 'kitchen', label: '厨房装箱', roles: ['kitchen'] },
  { key: 'courier', label: '配送路线', roles: ['courier'] },
  { key: 'frontdesk', label: '前台签收', roles: ['frontdesk'] },
  { key: 'review', label: '差异复核', roles: ['reviewer'] },
  { key: 'audit', label: '全局对账', roles: ['kitchen', 'courier', 'frontdesk', 'reviewer'] },
]
const tabs = computed(() => ALL_TABS.filter(t => t.roles.includes(session.user?.role)))

onMounted(async () => {
  users.value = await api.get('/users')
  if (session.user) tab.value = tabs.value[0]?.key || 'audit'
})

async function login() {
  try {
    const u = await api.post('/login', { name: name.value })
    session.set(u)
    tab.value = tabs.value[0]?.key || 'audit'
    err.value = ''
  } catch (e) { err.value = e.message }
}
function logout() { session.clear(); tab.value = '' }
</script>

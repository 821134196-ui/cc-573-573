<template>
  <LoginView v-if="!store.user" />
  <template v-else>
    <header class="app-header">
      <h1>🍱 园区团餐批次核对</h1>
      <span class="who">{{ roleLabel[store.user.role] }} · {{ store.user.name }}</span>
      <span class="spacer" />
      <button @click="logout">退出登录</button>
    </header>
    <div class="layout">
      <nav class="sidebar">
        <button v-for="m in menus" :key="m.key" :class="{ active: view === m.key }" @click="view = m.key">
          {{ m.label }}
        </button>
      </nav>
      <main class="content">
        <KitchenView v-if="view === 'kitchen'" :key="'k'+bump" @open="open" />
        <DispatchView v-else-if="view === 'dispatch'" :key="'d'+bump" @open="open" />
        <CourierView v-else-if="view === 'courier'" :key="'c'+bump" @open="open" />
        <FrontDeskView v-else-if="view === 'frontdesk'" :key="'f'+bump" @open="open" />
        <ReviewView v-else-if="view === 'review'" :key="'r'+bump" @open="open" />
      </main>
    </div>

    <BatchDetail v-if="detailId" :d="detail" :user="store.user" @close="detailId = null" @refresh="onRefresh" />
    <div v-if="store.toast" class="toast">{{ store.toast }}</div>
  </template>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { store, bootstrap, logout, ROLE_LABEL, notify } from './store.js'
import LoginView from './views/LoginView.vue'
import KitchenView from './views/KitchenView.vue'
import DispatchView from './views/DispatchView.vue'
import CourierView from './views/CourierView.vue'
import FrontDeskView from './views/FrontDeskView.vue'
import ReviewView from './views/ReviewView.vue'
import BatchDetail from './components/BatchDetail.vue'
import { api } from './api.js'

const roleLabel = ROLE_LABEL
const view = ref('')
const detailId = ref(null)
const detail = ref(null)
const bump = ref(0)

const menus = computed(() => {
  switch (store.user.role) {
    case 'kitchen': return [{ key: 'kitchen', label: '装箱与封签' }]
    case 'courier': return [{ key: 'courier', label: '配送与交接' }]
    case 'frontdesk': return [{ key: 'frontdesk', label: '签收与差异' }]
    case 'reviewer': return [{ key: 'review', label: '差异复核' }]
    case 'admin':
      return [
        { key: 'dispatch', label: '调度 / 改派' },
        { key: 'review', label: '差异复核' },
      ]
  }
  return []
})
watch(menus, (m) => { if (!m.find((x) => x.key === view.value)) view.value = m[0]?.key || '' }, { immediate: true })

async function open(id) {
  detailId.value = id
  detail.value = await api.get(`/batches/${id}`)
}
async function onRefresh() {
  bump.value++
  if (detailId.value) detail.value = await api.get(`/batches/${detailId.value}`)
}
bootstrap().catch((e) => notify(e.message))
</script>

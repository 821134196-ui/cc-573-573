<template>
  <div class="login-wrap">
    <div class="panel login-card">
      <div class="big">园区团餐批次核对系统</div>
      <div class="muted small mt8">装箱 · 封签 · 路线交接 · 签收获对 · 差异复核 · 数量守恒</div>
      <h3 style="color:var(--ink)">选择演示账号登录（本地模拟，无密码）</h3>
      <div style="display:flex;flex-direction:column;gap:8px">
        <button v-for="u in users" :key="u.id" class="row" style="justify-content:flex-start;padding:10px 12px" @click="doLogin(u.username)">
          <span class="chip" style="min-width:64px;text-align:center">{{ roleLabel[u.role] }}</span>
          <b>{{ u.name }}</b>
          <span class="muted small" style="margin-left:auto">{{ u.username }}</span>
        </button>
      </div>
    </div>
  </div>
</template>
<script setup>
import { store, login, ROLE_LABEL } from '../store.js'
const users = store.users
const roleLabel = ROLE_LABEL
async function doLogin(name) {
  try { await login(name) } catch (e) { alert(e.message) }
}
</script>

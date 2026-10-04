<template>
  <div>
    <div class="panel">
      <div class="row" style="justify-content:space-between">
        <h2 style="margin:0">配送员工作台 · {{ user.name }}</h2>
        <button @click="load">刷新</button>
      </div>
      <table class="mt12">
        <thead><tr><th>批次</th><th>企业</th><th>任务状态</th><th>封签</th><th class="num">盒数</th><th></th></tr></thead>
        <tbody>
          <tr v-for="a in tasks" :key="a.id" class="clickable" @click="$emit('open', a.batch_id)">
            <td>{{ a.Batch?.code }}</td>
            <td>{{ a.Batch?.Enterprise?.name }}</td>
            <td><span :class="['chip', cls(a.status)]">{{ label[a.status] }}</span></td>
            <td class="small">{{ (a.Batch?.boxes || []).map((b) => b.seal_no).join('、') || '—' }}</td>
            <td class="num">{{ a.Batch?.packed_total }}</td>
            <td><button class="ghost">处理</button></td>
          </tr>
          <tr v-if="!tasks.length"><td colspan="6" class="muted" style="text-align:center;padding:20px">当前没有待处理任务</td></tr>
        </tbody>
      </table>
    </div>
    <BatchList title="全部批次（只读）" :refresh-key="0" @open="$emit('open',$event)" />
  </div>
</template>
<script setup>
import { onMounted, ref } from 'vue'
import { api } from '../api.js'
import { store } from '../store.js'
import BatchList from '../components/BatchList.vue'

defineEmits(['open'])
const user = store.user
const tasks = ref([])
const label = { assigned: '待接收', accepted: '待出发', in_transit: '配送中', delivered: '待签收', failed: '签收失败' }
function cls(s) { return { delivered: 'ok', failed: 'danger', in_transit: 'info' }[s] || '' }
async function load() { tasks.value = await api.get('/routes/my') }
onMounted(load)
</script>

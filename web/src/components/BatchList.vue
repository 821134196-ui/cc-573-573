<template>
  <div class="panel">
    <div class="row" style="justify-content:space-between">
      <h2 style="margin:0">{{ title }}</h2>
      <div class="row gap8">
        <select v-model="statusFilter" @change="reload">
          <option value="">全部状态</option>
          <option v-for="(label, key) in statusLabel" :key="key" :value="key">{{ label }}</option>
        </select>
        <button @click="reload">刷新</button>
        <slot name="actions" />
      </div>
    </div>
    <table class="mt12">
      <thead>
        <tr>
          <th>批次号</th><th>企业</th><th>餐次</th><th class="num">盒数</th>
          <th class="num">金额</th><th>状态</th><th>封签</th><th>补送链</th><th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="b in batches" :key="b.id" class="clickable" @click="$emit('open', b.id)">
          <td>{{ b.code }}<span v-if="b.source_discrepancy_id" class="chip info" style="margin-left:6px">补送</span></td>
          <td>{{ b.Enterprise?.name || ('企业#' + b.enterprise_id) }}</td>
          <td class="small">{{ b.meal_date }} {{ mealLabel[b.meal_type] }}</td>
          <td class="num">{{ b.packed_total }}</td>
          <td class="num">{{ yuan(b.price_total) }}</td>
          <td><span :class="['chip', statusClass(b.status)]">{{ statusLabel[b.status] }}</span></td>
          <td>
            <span v-if="b.boxes?.length" class="small">{{ b.boxes.length }} 箱
              <span v-if="b.boxes.some(x => x.status==='broken')" class="chip danger" style="margin-left:4px">破损</span>
            </span>
            <span v-else class="muted small">—</span>
          </td>
          <td class="small muted">{{ b.root_id === b.id ? '原批次' : 'R' + b.resupply_seq }}</td>
          <td><button class="ghost" @click.stop="$emit('open', b.id)">查看</button></td>
        </tr>
        <tr v-if="!batches.length"><td colspan="9" class="muted" style="text-align:center;padding:24px">暂无批次</td></tr>
      </tbody>
    </table>
  </div>
</template>
<script setup>
import { onMounted, ref, watch } from 'vue'
import { api } from '../api.js'
import { STATUS_LABEL, MEAL_LABEL, yuan } from '../store.js'

const props = defineProps({ title: String, enterpriseScoped: Boolean, refreshKey: Number })
defineEmits(['open'])
const batches = ref([])
const statusFilter = ref('')
const statusLabel = STATUS_LABEL
const mealLabel = MEAL_LABEL

async function reload() {
  let path = '/batches?'
  if (statusFilter.value) path += `status=${statusFilter.value}&`
  batches.value = await api.get(path)
}
function statusClass(s) {
  return { closed: 'ok', signed: 'ok', resolving: 'warn', delivered: 'info',
    in_transit: 'info', sealed: 'gray', assigned: 'gray', draft: 'gray' }[s] || 'gray'
}
onMounted(reload)
watch(() => props.refreshKey, reload)
</script>

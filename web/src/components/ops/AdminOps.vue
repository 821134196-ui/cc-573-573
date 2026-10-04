<template>
  <div class="row">
    <template v-if="d.status === 'sealed'">
      <select v-model="routeId">
        <option :value="0" disabled>选择配送路线…</option>
        <option v-for="r in routes" :key="r.id" :value="r.id">{{ r.name }}（{{ r.Courier?.name || ('配送员#' + r.courier_id) }}）</option>
      </select>
      <input v-model="note" placeholder="交接说明（可选）" style="width:240px" />
      <button class="primary" @click="assign" :disabled="!routeId">指派路线</button>
    </template>
    <template v-else-if="reassignable">
      <select v-model="toRoute">
        <option :value="0" disabled>改派至路线…</option>
        <option v-for="r in routes" :key="r.id" :value="r.id">{{ r.name }}</option>
      </select>
      <input v-model="note" placeholder="改派原因，如车辆故障/无人签收" style="width:260px" />
      <button class="primary" @click="reassign" :disabled="!toRoute">途中改派</button>
      <span class="small muted">旧路线任务将关闭，同一批次仅一条有效路线。</span>
    </template>
    <span v-else class="small muted">当前状态不可调度（已封箱可指派；接收/途中/签收失败可改派；已完成不可改派）。</span>
  </div>
</template>
<script setup>
import { computed, ref } from 'vue'
import { api } from '../../api.js'
import { notify, store } from '../../store.js'

const props = defineProps({ d: Object })
const emit = defineEmits(['done'])
const routes = store.routes
const routeId = ref(0)
const toRoute = ref(0)
const note = ref('')

const active = computed(() => props.d.assignments?.find((a) => a.active))
const reassignable = computed(() => {
  const a = active.value
  if (!a) return false
  return ['assigned', 'accepted', 'in_transit', 'failed'].includes(a.status)
})

async function assign() {
  try {
    await api.post(`/dispatch/batches/${props.d.id}/assign`, { route_id: routeId.value, note: note.value })
    emit('done', '已指派路线')
  } catch (e) { notify(e.message) }
}
async function reassign() {
  try {
    await api.post(`/dispatch/batches/${props.d.id}/reassign`, { route_id: toRoute.value, note: note.value })
    emit('done', '已改派，旧路线任务关闭')
  } catch (e) { notify(e.message) }
}
</script>

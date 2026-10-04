<template>
  <div v-if="mine">
    <div class="row">
      <span class="chip info">当前有效路线任务：{{ assignLabel[mine.status] }}</span>
      <button v-if="mine.status === 'assigned'" class="primary" @click="act('accept','已接收封签完好的保温箱')">接收批次</button>
      <button v-if="mine.status === 'accepted'" class="primary" @click="act('depart','出发配送')">出发</button>
      <button v-if="mine.status === 'in_transit'" class="primary" @click="act('deliver','送达企业前台')">确认送达</button>
      <button v-if="['in_transit','delivered'].includes(mine.status)" class="danger" @click="noOne">登记无人签收</button>
    </div>
    <div class="small muted mt8">每次交接、改派、异常都会写入批次经过；改派后旧任务立即失效。</div>
  </div>
  <div v-else class="small muted">该批次当前没有分配给你的有效路线任务（可能已改派给其他路线）。</div>
</template>
<script setup>
import { computed } from 'vue'
import { api } from '../../api.js'
import { notify } from '../../store.js'

const props = defineProps({ d: Object, user: Object })
const emit = defineEmits(['done'])
const assignLabel = {
  assigned: '已指派待接收', accepted: '已接收待出发', in_transit: '配送途中',
  delivered: '已送达待签收', failed: '无人签收，等待处理', reassigned: '已改派',
}
const mine = computed(() =>
  props.d.assignments?.find((a) => a.active && a.courier_id === props.user.id))

async function act(path, note) {
  try {
    await api.post(`/routes/batches/${props.d.id}/${path}`, { note })
    emit('done', '操作已记录')
  } catch (e) { notify(e.message) }
}
async function noOne() {
  const note = prompt('无人签收说明（留下经过）', '约定时间前台无人，电话未接')
  if (note === null) return
  try {
    await api.post(`/routes/batches/${props.d.id}/no-one`, { note })
    emit('done', '已登记无人签收，等待调度改派')
  } catch (e) { notify(e.message) }
}
</script>

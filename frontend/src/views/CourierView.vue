<template>
  <div>
    <div class="panel">
      <h2>我的配送批次</h2>
      <p class="muted" style="margin:0">在途批次可登记封签破损、无人签收；途中改派会结束当前路线并留痕，同一批餐不会同时挂在两条路线。</p>
    </div>

    <BatchCard v-for="b in visible" :key="b.id" :b="b" :users="couriers">
      <template #default>
        <div class="panel" style="margin-top:12px;background:#fafbfd">
          <div class="row">
            <button v-if="b.status === 'in_transit'" @click="deliver(b.id)">送达企业</button>
            <button class="ghost" @click="openBroken(b.id)">登记封签破损</button>
            <button class="ghost" @click="openNobody(b.id)">登记无人签收</button>
            <button class="gray" v-if="b.status === 'in_transit'" @click="openReroute(b.id)">途中改派</button>
          </div>

          <div v-if="ctx.broken === b.id" class="row" style="margin-top:10px">
            <input v-model="ctx.boxNo" placeholder="箱号（可留空=全部）" style="width:170px" />
            <input v-model="ctx.note" placeholder="破损情况说明" style="flex:1" />
            <button @click="submitBroken(b.id)">提交破损</button>
          </div>
          <div v-if="ctx.nobody === b.id" class="row" style="margin-top:10px">
            <input v-model="ctx.note" placeholder="无人签收情况" style="flex:1" />
            <button @click="submitNobody(b.id)">提交无人签收</button>
          </div>
          <div v-if="ctx.reroute === b.id" class="row" style="margin-top:10px">
            <select v-model="ctx.newCourier">
              <option v-for="c in couriers" :key="c.id" :value="c.id">改派给：{{ c.name }}</option>
            </select>
            <input v-model="ctx.newRoute" placeholder="新路线名" />
            <input v-model="ctx.rerouteNote" placeholder="改派原因，如 车辆故障" style="flex:1" />
            <button @click="submitReroute(b.id)">确认改派</button>
          </div>
        </div>
      </template>
    </BatchCard>
    <p v-if="!visible.length" class="muted">当前没有与你相关的在途/待签收批次。</p>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, inject } from 'vue'
import { api } from '../api'
import BatchCard from '../components/BatchCard.vue'

const flash = inject('flash')
const batches = ref([]); const couriers = ref([])
const ctx = reactive({ broken: null, nobody: null, reroute: null, boxNo: '', note: '', newCourier: null, newRoute: '', rerouteNote: '' })

// 与当前配送员相关：自己接过路线的批次（含已签收，便于回看经过）
const visible = computed(() => batches.value.filter(b =>
  b.routes.some(r => r.courier_id === me())))

function me() { return JSON.parse(localStorage.getItem('campusmeal_user') || '{}').id }

function openBroken(id) { ctx.broken = id; ctx.note = ''; ctx.boxNo = '' }
function openNobody(id) { ctx.nobody = id; ctx.note = '' }
function openReroute(id) {
  ctx.reroute = id
  ctx.newCourier = couriers.value.find(c => c.id !== me())?.id || couriers.value[0]?.id
  ctx.newRoute = '临时改派线'; ctx.rerouteNote = ''
}
async function deliver(id) {
  try { await api.post(`/batches/${id}/deliver`); flash('已送达，等待前台签收'); await load() }
  catch (e) { flash(e.message, true) }
}
async function submitBroken(id) {
  try { await api.post(`/batches/${id}/seal-broken`, { box_no: ctx.boxNo, note: ctx.note }); flash('封签破损已登记并留痕'); await load() }
  catch (e) { flash(e.message, true) }
}
async function submitNobody(id) {
  try { await api.post(`/batches/${id}/nobody`, { note: ctx.note }); flash('无人签收已登记'); await load() }
  catch (e) { flash(e.message, true) }
}
async function submitReroute(id) {
  try {
    await api.post(`/batches/${id}/reroute`, { courier_id: ctx.newCourier, route_name: ctx.newRoute, note: ctx.rerouteNote })
    flash('改派完成，旧路线已结束'); await load()
  } catch (e) { flash(e.message, true) }
}

async function load() { batches.value = await api.get('/batches') }
onMounted(async () => {
  couriers.value = (await api.get('/users')).filter(u => u.role === 'courier')
  await load()
})
</script>

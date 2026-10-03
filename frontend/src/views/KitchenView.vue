<template>
  <div>
    <div class="panel">
      <h2>按企业 / 餐次生成装箱批次</h2>
      <div class="row">
        <select v-model="form.company_id">
          <option v-for="c in companies" :key="c.id" :value="c.id">{{ c.name }}（{{ c.address }}）</option>
        </select>
        <input v-model="form.meal_date" type="date" />
        <select v-model="form.meal_type">
          <option value="lunch">午餐</option><option value="breakfast">早餐</option><option value="dinner">晚餐</option>
        </select>
        <button @click="createBatch">按订单装箱</button>
      </div>
      <p class="muted" style="margin-bottom:0">装箱数量由订单应收自动锁定，厨房不能随意增减；前台也无权修改。</p>
    </div>

    <div class="panel">
      <h2>今日订单（应收依据）</h2>
      <table>
        <thead><tr><th>企业</th><th>日期</th><th>餐次</th><th>菜品明细</th><th>合计</th></tr></thead>
        <tbody>
          <tr v-for="o in orders" :key="o.id">
            <td>{{ o.company?.name }}</td><td>{{ o.meal_date }}</td><td>{{ mealLabel[o.meal_type] }}</td>
            <td><span v-for="l in o.lines" :key="l.id" class="tag gray" style="margin-right:4px">
              {{ l.dish_name }}×{{ l.ordered_qty }}</span></td>
            <td>{{ o.lines.reduce((s, l) => s + l.ordered_qty, 0) }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <h2>批次（最新在上）</h2>
    <BatchCard v-for="b in batches" :key="b.id" :b="b" :users="couriers">
      <template #default>
        <div v-if="b.status === 'packed'" class="panel" style="margin-top:12px;background:#fafbfd">
          <h3>登记保温箱封签</h3>
          <div v-for="(bx, i) in sealForms[b.id]" :key="i" class="row" style="margin-bottom:6px">
            <input v-model="bx.box_no" placeholder="箱号" style="width:120px" />
            <input v-model="bx.seal_no" placeholder="封签号" style="width:160px" />
            <input v-model.number="bx.boxed_qty" type="number" min="1" placeholder="盒数" style="width:90px" />
            <button class="gray" @click="sealForms[b.id].splice(i,1)">删除</button>
          </div>
          <div class="row">
            <button class="ghost" @click="addBox(b.id)">加一个保温箱</button>
            <button @click="submitSeals(b.id)">提交封签（盒数合计须 = {{ b.packed_qty }}）</button>
          </div>

          <h3 style="margin-top:14px">交接给配送员</h3>
          <div class="row">
            <select v-model="handover[b.id].courier_id">
              <option v-for="c in couriers" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
            <input v-model="handover[b.id].route_name" placeholder="路线名，如 园区北线" />
            <button @click="handoverBatch(b.id)">交接出餐</button>
          </div>
        </div>
      </template>
    </BatchCard>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, inject } from 'vue'
import { api } from '../api'
import { MEAL_LABEL as mealLabel } from '../labels'
import BatchCard from '../components/BatchCard.vue'

const flash = inject('flash')
const companies = ref([]); const orders = ref([]); const batches = ref([]); const couriers = ref([])
const form = reactive({ company_id: null, meal_date: '2026-10-03', meal_type: 'lunch' })
const sealForms = reactive({})
const handover = reactive({})

async function load() {
  orders.value = await api.get('/orders')
  batches.value = await api.get('/batches')
  if (!form.company_id && companies.value[0]) form.company_id = companies.value[0].id
  for (const b of batches.value) {
    if (b.status === 'packed') {
      if (!sealForms[b.id]) sealForms[b.id] = [{ box_no: 'BOX-' + b.batch_no + '-1', seal_no: 'SEAL-' + b.batch_no + '-1', boxed_qty: b.packed_qty }]
      if (!handover[b.id]) handover[b.id] = { courier_id: couriers.value[0]?.id, route_name: '园区北线' }
    }
  }
}
function addBox(id) { sealForms[id].push({ box_no: '', seal_no: '', boxed_qty: 1 }) }
async function createBatch() {
  try {
    await api.post('/batches', { ...form })
    flash('装箱批次已生成')
    await load()
  } catch (e) { flash(e.message, true) }
}
async function submitSeals(id) {
  try {
    await api.post(`/batches/${id}/seals`, { boxes: sealForms[id] })
    flash('封签已登记')
    await load()
  } catch (e) { flash(e.message, true) }
}
async function handoverBatch(id) {
  try {
    await api.post(`/batches/${id}/handover`, handover[id])
    flash('已交接给配送员')
    await load()
  } catch (e) { flash(e.message, true) }
}

onMounted(async () => {
  companies.value = await api.get('/companies')
  couriers.value = (await api.get('/users')).filter(u => u.role === 'courier')
  form.company_id = companies.value[0]?.id
  await load()
})
</script>

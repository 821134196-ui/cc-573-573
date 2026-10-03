<template>
  <div>
    <div class="panel">
      <h2>待我司签收 / 核对的批次</h2>
      <p class="muted" style="margin:0">
        并列查看应收、实收与差额；实收任何菜品都不能超过应收。前台可以提出差异，但不能修改厨房装箱数量。
      </p>
    </div>

    <BatchCard v-for="b in mine" :key="b.id" :b="b" :users="[]">
      <template #default>
        <div v-if="canSign(b)" class="panel" style="margin-top:12px;background:#fafbfd">
          <h3>签收核对</h3>
          <div class="row" style="margin-bottom:8px">
            <input v-model="sign[b.id].receiver" placeholder="签收人姓名" style="width:160px" />
            <label class="row" style="gap:6px">
              <input type="checkbox" v-model="sign[b.id].seal_intact" /> 封签完好
            </label>
          </div>
          <table>
            <thead><tr><th>菜品</th><th>应收</th><th>实收</th><th>差额</th></tr></thead>
            <tbody>
              <tr v-for="p in b.pack_items" :key="p.id">
                <td>{{ p.dish_name }}</td>
                <td>{{ p.qty }}</td>
                <td><input type="number" min="0" :max="p.qty" style="width:90px"
                  v-model.number="sign[b.id].qty[p.dish_name]" /></td>
                <td :class="diff(b, p) ? 'num-neg' : 'num-pos'">{{ diff(b, p) }}</td>
              </tr>
            </tbody>
          </table>
          <input v-model="sign[b.id].note" placeholder="备注（可选）" style="width:100%;margin-top:8px" />
          <div class="row" style="margin-top:10px">
            <button @click="submitSign(b.id)">确认签收（短少将自动生成差异单）</button>
          </div>
        </div>

        <div v-if="canAddDiff(b)" class="panel" style="margin-top:12px;background:#fafbfd">
          <h3>补充提出差异（不改动装箱数）</h3>
          <div class="row">
            <select v-model="extra[b.id].kind">
              <option value="wrong">错餐</option><option value="shortage">少盒</option>
              <option value="damage">封签破损</option><option value="nobody">无人签收</option>
            </select>
            <input v-model="extra[b.id].dish" placeholder="菜品（错餐时填写）" />
            <input v-model.number="extra[b.id].qty" type="number" min="0" placeholder="数量" style="width:90px" />
            <input v-model="extra[b.id].reason" placeholder="说明" style="flex:1" />
            <button @click="submitDiff(b.id)">提交差异</button>
          </div>
        </div>
      </template>
    </BatchCard>
    <p v-if="!mine.length" class="muted">暂无本企业的批次。</p>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, inject } from 'vue'
import { api, session } from '../api'
import BatchCard from '../components/BatchCard.vue'

const flash = inject('flash')
const batches = ref([])
const sign = reactive({})
const extra = reactive({})

const mine = computed(() => batches.value)
function canSign(b) {
  return !b.receipt && ['delivered', 'in_transit'].includes(b.status)
}
function canAddDiff(b) {
  return b.receipt || ['delivered', 'in_transit'].includes(b.status)
}
function diff(b, p) {
  const q = sign[b.id]?.qty[p.dish_name] ?? 0
  return q - p.qty
}

function ensureForms(b) {
  if (!sign[b.id]) {
    const qty = {}
    b.pack_items.forEach(p => { qty[p.dish_name] = p.qty })
    sign[b.id] = { receiver: session.user.name, seal_intact: true, qty, note: '' }
  }
  if (!extra[b.id]) extra[b.id] = { kind: 'wrong', dish: '', qty: 0, reason: '' }
}

async function submitSign(id) {
  const f = sign[id]
  const lines = Object.entries(f.qty).map(([dish_name, received_qty]) => ({ dish_name, received_qty }))
  try {
    await api.post(`/batches/${id}/sign`, {
      receiver_name: f.receiver, seal_intact: f.seal_intact, note: f.note, lines,
    })
    flash('已签收，差异已提交复核')
    await load()
  } catch (e) { flash(e.message, true) }
}
async function submitDiff(id) {
  const f = extra[id]
  try {
    await api.post(`/batches/${id}/discrepancies`, {
      kind: f.kind, dish_name: f.dish, short_qty: f.qty || 0, reason: f.reason,
    })
    flash('差异已提出，等待复核')
    await load()
  } catch (e) { flash(e.message, true) }
}

async function load() {
  batches.value = (await api.get('/batches')).filter(b => b.company_id === session.user.company_id)
  batches.value.forEach(ensureForms)
}
onMounted(load)
</script>

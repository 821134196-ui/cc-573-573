<template>
  <div>
    <div class="panel">
      <h2>差异复核台</h2>
      <p class="muted" style="margin:0">
        两阶段处理：先复核批准并确定处置方式（补送 / 损失核销 / 退款），再执行。处置数量必须与差异数量一致。
        补送会沿原批次生成补送批次（同企业同餐次），不另建无关订单；退款回执重复提交只入账一次。
      </p>
    </div>

    <div v-if="!openDisc.length" class="panel muted">暂无待处理差异。</div>

    <div v-for="d in openDisc" :key="d.id" class="panel">
      <div class="row" style="justify-content:space-between">
        <h3 style="margin:0">
          差异 #{{ d.id }} — {{ kindLabel[d.kind] }}
          <span v-if="d.dish_name">{{ d.dish_name }} <b v-if="d.short_qty">× {{ d.short_qty }} 盒</b></span>
        </h3>
        <span class="tag" :class="discCls[d.status]">{{ discLabel[d.status] }}</span>
      </div>
      <p class="muted" style="margin:6px 0">
        批次 {{ d.BatchNo }}（{{ d.company_name }}）｜原因：{{ d.reason || '—' }}
      </p>

      <!-- 第一阶段：审批 -->
      <div v-if="d.status === 'open'" class="row">
        <select v-model="forms[d.id].resolution">
          <option value="resupply">补送（沿原批次）</option>
          <option value="loss">损失核销</option>
          <option value="refund">退款（25 元/盒）</option>
        </select>
        <input v-model="forms[d.id].note" placeholder="复核意见" style="flex:1" />
        <button @click="review(d, true)">批准</button>
        <button class="danger" @click="review(d, false)">驳回</button>
      </div>

      <!-- 第二阶段：执行 -->
      <div v-else-if="d.status === 'approved'" class="row">
        <button v-if="d.resolution === 'resupply'" @click="resupply(d)">发起补送批次（{{ d.resolve_qty }} 盒）</button>
        <button v-else-if="d.resolution === 'loss'" class="danger" @click="loss(d)">确认损失核销（{{ d.resolve_qty }} 盒）</button>
        <template v-else-if="d.resolution === 'refund'">
          <input v-model="forms[d.id].receipt" placeholder="退款回执号，如 RCP-20261003-01" style="width:260px" />
          <button @click="refund(d, false)">提交退款（¥{{ (d.resolve_qty * 25).toFixed(2) }}）</button>
          <button class="gray" @click="refund(d, true)">重复提交同一回执（幂等测试）</button>
        </template>
      </div>
      <div v-else class="muted">已驳回。</div>
    </div>

    <h2>已处理 / 已驳回差异</h2>
    <div class="panel">
      <table>
        <thead><tr><th>#</th><th>批次</th><th>类型</th><th>数量</th><th>状态</th><th>处置</th><th>凭证</th></tr></thead>
        <tbody>
          <tr v-for="d in doneDisc" :key="d.id">
            <td>{{ d.id }}</td><td>{{ d.BatchNo }}</td><td>{{ kindLabel[d.kind] }}</td>
            <td>{{ d.short_qty }}</td>
            <td><span class="tag" :class="discCls[d.status]">{{ discLabel[d.status] }}</span></td>
            <td>{{ resLabel[d.resolution] || '—' }}</td>
            <td class="muted">
              <span v-if="d.resupply_batch_id">补送批次 #{{ d.resupply_batch_id }}</span>
              <span v-if="d.refund_receipt">退款回执 {{ d.refund_receipt }}</span>
              <span v-if="d.status==='rejected'">驳回：{{ d.review_note }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, inject } from 'vue'
import { api } from '../api'
import {
  DISC_LABEL as discLabel, DISC_CLS as discCls, KIND_LABEL as kindLabel, RES_LABEL as resLabel,
} from '../labels'

const flash = inject('flash')
const batches = ref([])
const forms = reactive({})

const allDisc = computed(() => batches.value.flatMap(b => b.discrepancies.map(d => ({
  ...d, BatchNo: b.batch_no, company_name: b.company_name, refund_receipt: b.refunds.find(r => r.discrepancy_id === d.id)?.receipt_no,
}))))
const openDisc = computed(() => allDisc.value.filter(d => ['open', 'approved'].includes(d.status)))
const doneDisc = computed(() => allDisc.value.filter(d => ['processed', 'rejected'].includes(d.status)))

function ensure(d) {
  if (!forms[d.id]) forms[d.id] = { resolution: 'resupply', note: '', receipt: 'RCP-' + Date.now().toString().slice(-8) + '-' + d.id }
}

async function review(d, approve) {
  ensure(d)
  const body = { approve, resolution: forms[d.id].resolution, note: forms[d.id].note }
  try {
    await api.post(`/discrepancies/${d.id}/review`, body)
    flash(approve ? '已批准，进入执行阶段' : '已驳回')
    await load()
  } catch (e) { flash(e.message, true) }
}
async function resupply(d) {
  try {
    const b = await api.post(`/discrepancies/${d.id}/resupply`, {})
    flash(`补送批次 ${b.batch_no} 已生成，挂回原餐次`)
    await load()
  } catch (e) { flash(e.message, true) }
}
async function loss(d) {
  try { await api.post(`/discrepancies/${d.id}/loss`, {}); flash('损失已核销，原批次数量平衡'); await load() }
  catch (e) { flash(e.message, true) }
}
async function refund(d, duplicate) {
  try {
    const r = await api.post(`/discrepancies/${d.id}/refund`, { receipt_no: forms[d.id].receipt })
    flash(duplicate || r.deduplicated ? '回执已入账过，本次去重，未重复退款' : '退款成功，回执已入账')
    await load()
  } catch (e) { flash(e.message, true) }
}

async function load() {
  batches.value = await api.get('/batches')
  allDisc.value.forEach(ensure)
}
onMounted(load)
</script>

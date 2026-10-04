<template>
  <div class="panel">
    <div class="row" style="justify-content:space-between">
      <h2 style="margin:0">差异复核工作台</h2>
      <div class="row gap8">
        <select v-model="filter" @change="load">
          <option value="open">待复核</option>
          <option value="">全部</option>
          <option value="approved">已批准</option>
          <option value="rejected">已驳回</option>
        </select>
        <button @click="load">刷新</button>
      </div>
    </div>

    <div v-for="d in discs" :key="d.id" class="mt12" style="border:1px solid var(--line);border-radius:10px;padding:14px">
      <div class="row">
        <b>差异 #{{ d.id }}</b>
        <span class="chip warn">{{ kindLabel(d.kind) }}</span>
        <span :class="['chip', d.status === 'open' ? 'warn' : d.status === 'approved' ? 'ok' : 'gray']">
          {{ d.status === 'open' ? '待复核' : d.status === 'approved' ? '已批准' : '已驳回' }}
        </span>
        <span class="small muted">批次 #{{ d.batch_id }} · 提出人 #{{ d.raised_by }} · {{ d.note }}</span>
        <button class="ghost" style="margin-left:auto" @click="$emit('open', d.batch_id)">打开批次</button>
      </div>

      <table class="mt8">
        <thead>
          <tr><th>餐品</th><th class="num">应收</th><th class="num">实收</th><th class="num">短少/错餐</th>
            <th v-if="d.status==='open'" class="num">补送</th>
            <th v-if="d.status==='open'" class="num">损失</th>
            <th v-if="d.status==='open'" class="num">退款</th>
            <th v-else class="num">补/损/退</th></tr>
        </thead>
        <tbody>
          <tr v-for="l in d.lines" :key="l.id">
            <td>{{ l.name }}<span v-if="l.wrong" class="chip warn" style="margin-left:6px">错餐-退回厨房</span></td>
            <td class="num">{{ l.expected_qty }}</td><td class="num">{{ l.received_qty }}</td>
            <td class="num"><b>{{ l.short_qty }}</b></td>
            <template v-if="d.status === 'open'">
              <td v-if="!l.wrong" class="num"><input v-model.number="alloc[l.id].resupply" type="number" min="0" style="width:64px" /></td>
              <td v-if="l.wrong" class="num muted">—</td>
              <td v-if="!l.wrong" class="num"><input v-model.number="alloc[l.id].loss" type="number" min="0" style="width:64px" /></td>
              <td v-if="l.wrong" class="num muted">—</td>
              <td v-if="!l.wrong" class="num"><input v-model.number="alloc[l.id].refund" type="number" min="0" style="width:64px" /></td>
              <td v-if="l.wrong" class="num muted">—</td>
            </template>
            <td v-else class="num small">{{ l.resupply_qty }} / {{ l.loss_qty }} / {{ l.refund_qty }}</td>
          </tr>
        </tbody>
      </table>

      <div v-if="d.status === 'open'" class="row end mt12 gap8">
        <label class="field" style="flex:1">复核意见
          <input v-model="notes[d.id]" placeholder="如：监控确认少装 1 盒，另 1 盒退款" />
        </label>
        <button @click="quickFill(d, 'resupply')">全部补送</button>
        <button @click="quickFill(d, 'refund')">全部退款</button>
        <button class="danger" @click="reject(d)">驳回</button>
        <button class="primary" @click="approve(d)">批准并核对数量</button>
      </div>

      <div v-if="d.resolution" class="small muted mt8">
        复核结论：{{ d.resolution.note }}
        <span v-if="d.resolution.resupply_batch_id"> · 补送批次 #{{ d.resolution.resupply_batch_id }}（沿原批次树处理）</span>
        <span v-if="d.resolution.refund">
          · 退款 {{ yuan(d.resolution.refund.amount) }}
          <span :class="['chip', d.resolution.refund.status === 'posted' ? 'ok' : 'warn']">
            {{ d.resolution.refund.status === 'posted' ? '已入账 ' + d.resolution.refund.gateway_receipt : '待支付回执' }}
          </span>
          <button v-if="d.resolution.refund.status !== 'posted'" class="ghost" @click="postReceipt(d.resolution)">模拟支付回执入账</button>
        </span>
      </div>
    </div>
  </div>
</template>
<script setup>
import { onMounted, reactive, ref } from 'vue'
import { api } from '../api.js'
import { notify, yuan } from '../store.js'

const emit = defineEmits(['open'])
const discs = ref([])
const filter = ref('open')
const alloc = reactive({})
const notes = reactive({})

function initAlloc(d) {
  for (const l of d.lines) {
    if (!alloc[l.id] && !l.wrong) alloc[l.id] = { resupply: l.short_qty, loss: 0, refund: 0 }
  }
}
async function load() {
  const path = filter.value ? `/discrepancies?status=${filter.value}` : '/discrepancies'
  discs.value = await api.get(path)
  discs.value.forEach(initAlloc)
}
function quickFill(d, kind) {
  for (const l of d.lines) {
    if (l.wrong) continue
    alloc[l.id] = { resupply: 0, loss: 0, refund: 0 }
    alloc[l.id][kind] = l.short_qty
  }
}
async function approve(d) {
  const lines = d.lines
    .filter((l) => !l.wrong)
    .map((l) => ({ line_id: l.id, resupply_qty: alloc[l.id]?.resupply || 0, loss_qty: alloc[l.id]?.loss || 0, refund_qty: alloc[l.id]?.refund || 0 }))
  try {
    const res = await api.post(`/review/discrepancies/${d.id}/approve`, { lines, note: notes[d.id] || '', auto_post_pay: false })
    if (res.conservation?.balanced) notify('已批准，数量守恒核对通过')
    await load()
  } catch (e) { notify(e.message) }
}
async function reject(d) {
  try {
    await api.post(`/review/discrepancies/${d.id}/reject`, { note: notes[d.id] || '差异不成立' })
    notify('已驳回，短少数计入争议')
    await load()
  } catch (e) { notify(e.message) }
}
async function postReceipt(res0) {
  try {
    const r = await api.post(`/review/resolutions/${res0.id}/refund-receipt`, {
      gateway_receipt: 'GW-' + Math.floor(Math.random() * 1e8),
    })
    notify(r.duplicate ? '该退款已入账，重复回执未重复记账' : '支付回执已入账')
    await load()
  } catch (e) { notify(e.message) }
}
function kindLabel(k) {
  return k.split('+').map((x) => ({ short: '少盒', wrong: '错餐', broken: '封签破损', no_one: '无人签收' }[x] || x)).join('+')
}
onMounted(load)
</script>

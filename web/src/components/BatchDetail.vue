<template>
  <Modal wide @close="$emit('close')">
    <div class="row" style="justify-content: space-between">
      <div>
        <h2>批次 {{ d.code }}</h2>
        <div class="muted small">
          {{ d.Enterprise?.Name || enterpriseName }} · {{ d.meal_date }} {{ mealLabel[d.meal_type] }}
          <span :class="['chip', statusClass(d.status)]">{{ statusLabel[d.status] }}</span>
          <span v-if="d.parent_id" class="chip info">补送批次 R{{ d.resupply_seq }}</span>
        </div>
      </div>
      <div class="row">
        <button @click="loadConsv">刷新守恒</button>
        <button @click="$emit('close')">关闭</button>
      </div>
    </div>

    <div class="grid2 mt12">
      <div class="panel" style="margin:0">
        <h2>装箱清单（厨房）<span class="muted small">合计 {{ d.packed_total }} 盒 / {{ yuan(d.price_total) }}</span></h2>
        <table>
          <thead><tr><th>餐品</th><th class="num">单价</th><th class="num">装箱数</th></tr></thead>
          <tbody>
            <tr v-for="it in d.items" :key="it.id">
              <td>{{ it.name }}</td><td class="num">{{ yuan(it.unit_price) }}</td><td class="num"><b>{{ it.packed_qty }}</b></td>
            </tr>
          </tbody>
        </table>
        <h3>保温箱与封签</h3>
        <table v-if="d.boxes?.length">
          <thead><tr><th>箱号</th><th>封签号</th><th>状态</th><th>破损说明</th></tr></thead>
          <tbody>
            <tr v-for="b in d.boxes" :key="b.id">
              <td>{{ b.code }}</td>
              <td>{{ b.seal_no }}</td>
              <td><span :class="['chip', b.status === 'broken' ? 'danger' : 'ok']">{{ b.status === 'broken' ? '破损' : '完好' }}</span></td>
              <td class="small muted">{{ b.broken_note }}</td>
            </tr>
          </tbody>
        </table>
        <div v-else class="muted small">尚未封箱</div>
      </div>

      <div class="panel" style="margin:0">
        <h2>路线交接</h2>
        <table v-if="d.assignments?.length">
          <thead><tr><th>#</th><th>路线</th><th>状态</th><th>有效</th></tr></thead>
          <tbody>
            <tr v-for="a in d.assignments" :key="a.id">
              <td>{{ a.seq }}</td><td>#{{ a.route_id }}</td>
              <td><span class="chip" :class="assignClass(a.status)">{{ assignLabel[a.status] }}</span></td>
              <td>{{ a.active ? '是' : '—' }}</td>
            </tr>
          </tbody>
        </table>
        <div v-else class="muted small">尚未指派路线</div>

        <h3>签收回执（前台）</h3>
        <template v-if="receipt">
          <div class="small">签收人：{{ receipt.receiver_name }} · 封签完好：{{ receipt.seal_ok ? '是' : '否' }} {{ receipt.seal_note }}</div>
          <table class="mt8">
            <thead><tr><th>餐品</th><th class="num">应收</th><th class="num">实收</th><th class="num">差额</th></tr></thead>
            <tbody>
              <tr v-for="l in receipt.lines" :key="l.id">
                <td>{{ l.name }}<span v-if="l.wrong" class="chip warn" style="margin-left:6px">错餐</span></td>
                <td class="num">{{ l.expected_qty }}</td>
                <td class="num">{{ l.received_qty }}</td>
                <td class="num">
                  <span v-if="l.wrong" class="muted">—</span>
                  <span v-else-if="l.expected_qty === l.received_qty" class="diff-pos">0</span>
                  <span v-else class="diff-neg">{{ l.received_qty - l.expected_qty }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </template>
        <div v-else class="muted small">尚未签收</div>
      </div>
    </div>

    <Conservation class="panel mt12" :rep="consv" />

    <!-- 角色操作区 -->
    <div class="panel mt12">
      <h2>操作（{{ roleLabel[user.role] }}）</h2>
      <KitchenOps v-if="user.role === 'kitchen'" :d="d" @done="done" />
      <AdminOps v-else-if="user.role === 'admin'" :d="d" @done="done" />
      <CourierOps v-else-if="user.role === 'courier'" :d="d" :user="user" @done="done" />
      <SignOps v-else-if="user.role === 'frontdesk' && canSign" :d="d" :user="user" @done="done" />
      <SealBrokenOps v-if="['courier','frontdesk'].includes(user.role)" :d="d" @done="done" />
      <div v-if="!hasOps" class="muted small">当前角色在该状态下没有可执行操作，可在对应工作台处理差异单。</div>
    </div>

    <div class="panel mt12">
      <h2>差异与复核</h2>
      <template v-if="d.discrepancies?.length">
        <div v-for="x in d.discrepancies" :key="x.id" class="mt8" style="border-bottom:1px solid var(--line);padding-bottom:8px">
          <div class="row">
            <span class="chip warn">差异 #{{ x.id }} {{ diffKind(x.kind) }}</span>
            <span :class="['chip', x.status === 'open' ? 'warn' : x.status === 'approved' ? 'ok' : 'gray']">
              {{ statusDiff[x.status] }}
            </span>
            <span class="muted small">{{ x.note }}</span>
          </div>
          <table class="mt8">
            <thead><tr><th>餐品</th><th class="num">应收</th><th class="num">实收</th><th class="num">短少</th>
              <th class="num">补送</th><th class="num">损失</th><th class="num">退款</th></tr></thead>
            <tbody>
              <tr v-for="l in x.lines" :key="l.id">
                <td>{{ l.name }}<span v-if="l.wrong" class="chip warn" style="margin-left:6px">错餐</span></td>
                <td class="num">{{ l.expected_qty }}</td><td class="num">{{ l.received_qty }}</td>
                <td class="num"><b>{{ l.short_qty }}</b></td>
                <td class="num">{{ l.resupply_qty }}</td><td class="num">{{ l.loss_qty }}</td><td class="num">{{ l.refund_qty }}</td>
              </tr>
            </tbody>
          </table>
          <div v-if="x.resolution" class="small muted mt8">
            复核 #{{ x.resolution.id }}：{{ x.resolution.note }}
            <template v-if="x.resolution.resupply_batch_id"> · 已发起补送批次 #{{ x.resolution.resupply_batch_id }}</template>
            <template v-if="x.resolution.refund">
              · 退款 {{ yuan(x.resolution.refund.amount) }}
              <span :class="['chip', x.resolution.refund.status === 'posted' ? 'ok' : 'warn']">
                {{ x.resolution.refund.status === 'posted' ? '已入账 ' + x.resolution.refund.gateway_receipt : '待入账' }}
              </span>
            </template>
          </div>
        </div>
      </template>
      <div v-else class="muted small">暂无差异单</div>
    </div>

    <div class="panel mt12">
      <h2>经过时间线</h2>
      <div class="timeline">
        <div v-for="(e, i) in events" :key="'e'+i" class="ev">
          <span class="t">{{ fmt(e.created_at) }}</span>
          <b>{{ evLabel[e.type] || e.type }}</b> · {{ e.actor_name }}
          <span v-if="e.note" class="muted"> — {{ e.note }}</span>
        </div>
        <div v-for="(a, i) in d.audits" :key="'a'+i" class="ev">
          <span class="t">{{ fmt(a.created_at) }}</span>
          <b>{{ a.action }}</b> · {{ a.actor_name }}
          <span v-if="a.detail" class="muted"> — {{ a.detail }}</span>
        </div>
      </div>
    </div>
  </Modal>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { api } from '../api.js'
import { notify, STATUS_LABEL, ROLE_LABEL, MEAL_LABEL, DIFF_LABEL, yuan } from '../store.js'
import Modal from './Modal.vue'
import Conservation from './Conservation.vue'
import KitchenOps from './ops/KitchenOps.vue'
import AdminOps from './ops/AdminOps.vue'
import CourierOps from './ops/CourierOps.vue'
import SignOps from './ops/SignOps.vue'
import SealBrokenOps from './ops/SealBrokenOps.vue'

const props = defineProps({ d: Object, user: Object })
const emit = defineEmits(['close'])

const statusLabel = STATUS_LABEL
const roleLabel = ROLE_LABEL
const mealLabel = MEAL_LABEL
const statusDiff = { open: '待复核', approved: '已批准', rejected: '已驳回' }
const assignLabel = {
  assigned: '已指派', accepted: '已接收', in_transit: '配送中',
  delivered: '已送达', reassigned: '已改派', failed: '签收失败',
}
const evLabel = {
  assign: '指派路线', accept: '配送接收', depart: '出发', deliver: '送达',
  sign: '前台签收', reassign: '途中改派', no_one: '无人签收', seal_broken: '封签破损',
}

const enterpriseName = computed(() => props.d.enterprise_name)
const receipt = computed(() => props.d.receipt)
const consv = ref(null)
const events = ref([])

const canSign = computed(() =>
  props.user.enterprise_id === props.d.enterprise_id &&
  ['delivered', 'resolving'].includes(props.d.status))

const hasOps = computed(() => {
  const u = props.user
  if (u.role === 'kitchen') return props.d.status === 'draft'
  if (u.role === 'admin') return true
  if (u.role === 'courier') {
    return props.d.assignments?.some((a) => a.active && a.courier_id === u.id &&
      ['assigned', 'accepted', 'in_transit', 'delivered', 'failed'].includes(a.status))
  }
  return canSign.value
})

async function loadConsv() {
  consv.value = await api.get(`/batches/${props.d.id}/conservation`)
}
async function loadEvents() {
  const t = await api.get(`/batches/${props.d.id}/timeline`)
  events.value = t.route_events
}
function done(msg) {
  if (msg) notify(msg)
  emit('refresh')
}
function statusClass(s) {
  return { closed: 'ok', signed: 'ok', resolving: 'warn', delivered: 'info',
    in_transit: 'info', sealed: 'gray', assigned: 'gray', draft: 'gray' }[s] || 'gray'
}
function assignClass(s) {
  return { delivered: 'ok', reassigned: 'gray', failed: 'danger', in_transit: 'info' }[s] || ''
}
function diffKind(k) {
  return k.split('+').map((x) => ({ short: '少盒', wrong: '错餐', broken: '封签破损', no_one: '无人签收' }[x] || x)).join('+')
}
function fmt(t) { return (t || '').replace('T', ' ').slice(0, 16) }

onMounted(() => { loadConsv(); loadEvents() })
</script>

<template>
  <div>
    <div class="panel">
      <h2>全局数量守恒对账</h2>
      <div class="metric">
        <div>累计装箱 <b>{{ audit.total_packed }}</b></div>
        <div>累计签收 <b class="num-pos">{{ audit.total_signed }}</b></div>
        <div>核销/损失 <b class="num-neg">{{ audit.total_lost }}</b></div>
        <div>在途待结 <b>{{ audit.total_outstanding }}</b></div>
      </div>
      <p :class="audit.all_balanced ? 'ok-msg' : 'err'" style="font-size:15px">
        {{ audit.all_balanced ? '✓ 全部批次数量平衡：装箱 = 签收 + 核销 + 在途，餐盒没有凭空增加。' : '✗ 存在不平衡批次！' }}
      </p>
      <p :class="audit.refund_receipt_unique ? 'ok-msg' : 'err'">
        {{ audit.refund_receipt_unique ? '✓ 退款回执全局唯一，无重复入账。' : '✗ 存在重复退款回执：' + audit.duplicate_refund_receipts?.join('、') }}
      </p>
      <p v-if="audit.unbalanced?.length" class="err">不平衡批次：{{ audit.unbalanced.join('、') }}</p>
      <p v-if="audit.orphan_resupplies?.length" class="err">无差异依据的补送批次：{{ audit.orphan_resupplies.join('、') }}</p>
      <button class="ghost" @click="load">刷新对账</button>
    </div>

    <div class="panel">
      <h2>本地模拟服务记录</h2>
      <h3>支付服务退款回执（含重复提交次数）</h3>
      <table>
        <thead><tr><th>回执号</th><th>金额(元)</th><th>盒数</th><th>提交次数</th><th>状态</th></tr></thead>
        <tbody>
          <tr v-for="r in logs.payment" :key="r.ID">
            <td>{{ r.ReceiptNo }}</td><td>{{ (r.Amount / 100).toFixed(2) }}</td>
            <td>{{ r.Qty }}</td>
            <td><span :class="r.CallCount > 1 ? 'tag warn' : ''">{{ r.CallCount }}</span></td>
            <td>{{ r.Status }}</td>
          </tr>
          <tr v-if="!logs.payment.length"><td colspan="5" class="muted">暂无退款</td></tr>
        </tbody>
      </table>
      <h3 style="margin-top:14px">配送调度轨迹</h3>
      <table>
        <thead><tr><th>批次</th><th>动作</th><th>路线</th><th>配送员</th><th>轨迹单号</th></tr></thead>
        <tbody>
          <tr v-for="l in logs.delivery" :key="l.ID">
            <td>{{ l.BatchNo }}</td>
            <td><span class="tag info">{{ { dispatch: '派车', reroute: '改派', complete: '完成' }[l.Action] }}</span></td>
            <td>{{ l.RouteName || '—' }}</td><td>{{ l.CourierName || '—' }}</td><td>{{ l.TrackingNo }}</td>
          </tr>
          <tr v-if="!logs.delivery.length"><td colspan="5" class="muted">暂无调度记录</td></tr>
        </tbody>
      </table>
    </div>

    <h2>全部批次</h2>
    <BatchCard v-for="b in batches" :key="b.id" :b="b" :users="users" />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'
import BatchCard from '../components/BatchCard.vue'

const audit = ref({ all_balanced: true, total_packed: 0, total_signed: 0, total_lost: 0, total_outstanding: 0 })
const logs = ref({ payment: [], delivery: [] })
const batches = ref([])
const users = ref([])

async function load() {
  audit.value = await api.get('/audit')
  logs.value = await api.get('/mock-logs')
  batches.value = await api.get('/batches')
}
onMounted(async () => {
  users.value = await api.get('/users')
  await load()
})
</script>

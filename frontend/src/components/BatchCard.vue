<template>
  <div class="panel">
    <div class="row" style="justify-content: space-between">
      <div>
        <h3 style="margin:0">
          批次 {{ b.batch_no }}
          <span class="tag" :class="statusCls[b.status]">{{ statusLabel[b.status] }}</span>
          <span v-if="b.kind === 'resupply'" class="tag warn">补送</span>
        </h3>
        <div class="muted" style="margin-top:4px">
          {{ b.company_name }} · {{ b.meal_date }} · {{ mealLabel[b.meal_type] }}
          <template v-if="b.root_batch_id"> · 源自批次 #{{ b.root_batch_id }}</template>
        </div>
      </div>
      <div>
        <div class="metric" style="margin:0">
          <div>应收 <b>{{ b.packed_qty }}</b></div>
          <div>签收 <b class="num-pos">{{ b.signed_qty }}</b></div>
          <div>核销/损失 <b class="num-neg">{{ b.lost_qty }}</b></div>
          <div>在途待结 <b :class="b.conservation.outstanding ? 'num-neg' : 'num-pos'">{{ b.conservation.outstanding }}</b></div>
        </div>
        <div :class="b.conservation.balanced ? 'ok-msg' : 'err'">
          {{ b.conservation.balanced ? '✓ 数量平衡：装箱 = 签收 + 核销 + 在途' : '✗ 数量不平衡！' }}
        </div>
      </div>
    </div>

    <!-- 装箱清单（仅厨房可创建，所有角色只读） -->
    <h3 style="margin-top:14px">装箱清单</h3>
    <table>
      <thead><tr><th>菜品</th><th style="width:110px">装箱(应收)</th></tr></thead>
      <tbody>
        <tr v-for="p in b.pack_items" :key="p.id">
          <td>{{ p.dish_name }}</td><td>{{ p.qty }}</td>
        </tr>
      </tbody>
    </table>

    <!-- 保温箱与封签 -->
    <template v-if="b.boxes.length">
      <h3 style="margin-top:14px">保温箱 / 封签</h3>
      <table>
        <thead><tr><th>箱号</th><th>封签号</th><th>盒数</th><th>封签状态</th></tr></thead>
        <tbody>
          <tr v-for="x in b.boxes" :key="x.id">
            <td>{{ x.box_no }}</td><td>{{ x.seal_no }}</td><td>{{ x.boxed_qty }}</td>
            <td><span class="tag" :class="x.seal_status === 'sealed' ? 'ok' : 'bad'">
              {{ { sealed: '完好', broken: '破损', resealed: '重封' }[x.seal_status] }}</span></td>
          </tr>
        </tbody>
      </table>
    </template>

    <!-- 路线经过 -->
    <template v-if="b.routes.length">
      <h3 style="margin-top:14px">路线与交接</h3>
      <table>
        <thead><tr><th>路线</th><th>配送员</th><th>状态</th><th>说明</th></tr></thead>
        <tbody>
          <tr v-for="r in b.routes" :key="r.id">
            <td>{{ r.route_name }}</td>
            <td>{{ courierName(r.courier_id) }}</td>
            <td><span class="tag" :class="r.active ? 'warn' : 'gray'">{{ r.active ? '进行中' : '已结束' }}</span></td>
            <td class="muted">{{ r.note }}</td>
          </tr>
        </tbody>
      </table>
    </template>

    <!-- 签收核对：应收/实收/差额并列 -->
    <template v-if="b.receipt">
      <h3 style="margin-top:14px">前台签收核对（签收人：{{ b.receipt.receiver_name }}）</h3>
      <table>
        <thead><tr><th>菜品</th><th>应收</th><th>实收</th><th>差额</th></tr></thead>
        <tbody>
          <tr v-for="l in b.receipt.lines" :key="l.id">
            <td>{{ l.dish_name }}</td>
            <td>{{ l.expected_qty }}</td>
            <td>{{ l.received_qty }}</td>
            <td :class="l.expected_qty - l.received_qty ? 'num-neg' : 'num-pos'">
              {{ l.received_qty - l.expected_qty }}</td>
          </tr>
        </tbody>
      </table>
      <p class="muted" style="margin:6px 0 0">
        封签：{{ b.receipt.seal_intact ? '完好' : '破损' }}；合计应收 {{ b.packed_qty }} / 实收 {{ b.receipt.signed_qty }}
        / 差额 {{ b.packed_qty - b.receipt.signed_qty }}
        <template v-if="b.receipt.note">；备注：{{ b.receipt.note }}</template>
      </p>
    </template>

    <!-- 差异与处理 -->
    <template v-if="b.discrepancies.length">
      <h3 style="margin-top:14px">差异单与处理</h3>
      <div v-for="d in b.discrepancies" :key="d.id" class="diff-card">
        <div class="row" style="justify-content:space-between">
          <div>
            #{{ d.id }} <b>{{ kindLabel[d.kind] }}</b>
            <span v-if="d.dish_name">{{ d.dish_name }}</span>
            <span v-if="d.short_qty"> × {{ d.short_qty }} 盒</span>
            <span class="tag" :class="discCls[d.status]" style="margin-left:6px">{{ discLabel[d.status] }}</span>
          </div>
          <div class="muted">{{ d.reason }}</div>
        </div>
        <div v-if="d.resolution" class="muted" style="margin-top:4px">
          复核决定：{{ resLabel[d.resolution] }} {{ d.resolve_qty ? d.resolve_qty + ' 盒' : '' }}
          <template v-if="d.review_note">｜{{ d.review_note }}</template>
          <template v-if="d.resupply_batch_id">｜补送批次 #{{ d.resupply_batch_id }}</template>
        </div>
      </div>
    </template>

    <!-- 事件经过 -->
    <h3 style="margin-top:14px">全程留痕</h3>
    <ul class="timeline">
      <li v-for="e in [...b.events].reverse()" :key="e.id">
        <div>{{ e.detail }}</div>
        <div class="t-meta">{{ e.actor }} · {{ fmt(e.created_at) }}</div>
      </li>
    </ul>

    <!-- 子批次（补送） -->
    <template v-if="b.children?.length">
      <h3 style="margin-top:14px">沿原批次发起的补送</h3>
      <p v-for="c in b.children" :key="c.id" class="muted">
        补送批次 {{ c.batch_no }}（{{ c.packed_qty }} 盒）— 状态：{{ statusLabel[c.status] }}
      </p>
    </template>

    <slot :batch="b" />
  </div>
</template>

<script setup>
import { inject } from 'vue'
import {
  STATUS_LABEL as statusLabel, STATUS_CLS as statusCls, MEAL_LABEL as mealLabel,
  DISC_LABEL as discLabel, DISC_CLS as discCls, KIND_LABEL as kindLabel, RES_LABEL as resLabel,
} from '../labels'

const props = defineProps({
  b: { type: Object, required: true },
  users: { type: Array, default: () => [] },
})
inject('flash', () => {})

function courierName(id) {
  return props.users.find(u => u.id === id)?.name || `#${id}`
}
function fmt(s) { return new Date(s).toLocaleString('zh-CN', { hour12: false }) }
</script>

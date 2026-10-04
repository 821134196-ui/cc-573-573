<template>
  <div v-if="rep">
    <div class="row" style="justify-content: space-between">
      <strong>数量守恒核对（批次树 #{{ rep.root_id }}）</strong>
      <span :class="['chip', rep.balanced ? 'ok' : 'danger']">
        {{ rep.balanced ? '✓ 账实相符' : '✗ 数量异常' }}
      </span>
    </div>
    <div class="consv mt8">
      <div class="bucket"><b>{{ rep.produced }}</b><span>厨房装箱产出</span></div>
      <div class="bucket"><b>=</b><span>&nbsp;</span></div>
      <div class="bucket"><b>{{ rep.received }}</b><span>正常实收</span></div>
      <div class="bucket"><b>{{ rep.in_flight }}</b><span>在途</span></div>
      <div class="bucket"><b>{{ rep.open_claim }}</b><span>待复核差异</span></div>
      <div class="bucket"><b>{{ rep.disputed }}</b><span>争议驳回</span></div>
      <div class="bucket"><b>{{ rep.lost }}</b><span>损失核销</span></div>
      <div class="bucket"><b>{{ rep.refund_wait }}</b><span>待入账退款</span></div>
      <div class="bucket"><b>{{ rep.refunded }}</b><span>已退款</span></div>
      <div class="bucket"><b>{{ rep.resupplied }}</b><span>补送核销</span></div>
    </div>
    <div class="small muted mt8">
      去向合计 {{ rep.accounted }}
      <template v-if="rep.pending_pack_planned">；另有 {{ rep.pending_pack_planned }} 盒在补送批次装箱计划中（未封箱，不计入产出）</template>
      ｜补送依据：批准 {{ rep.resupply_approved }} / 已装箱 {{ rep.resupply_packed }}
    </div>
    <div v-if="rep.wrong_received + rep.wrong_return + rep.wrong_kept > 0" class="small muted mt8">
      错餐实物：签收 {{ rep.wrong_received }} ＝ 暂扣 {{ rep.wrong_held }} ＋ 退回厨房 {{ rep.wrong_return }} ＋ 驳回自留 {{ rep.wrong_kept }}
    </div>
    <div v-if="!rep.balanced" class="small mt8" style="color: var(--danger)">{{ rep.detail }}</div>
  </div>
</template>
<script setup>
defineProps({ rep: Object })
</script>

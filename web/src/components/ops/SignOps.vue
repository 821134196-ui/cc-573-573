<template>
  <div>
    <div class="small muted">应收数量取自厨房装箱清单（只读）；请并列填写实收。实收超过装箱将被系统拒绝。</div>
    <table class="mt8">
      <thead><tr><th>餐品</th><th class="num">应收</th><th class="num">实收</th><th class="num">差额</th><th>备注</th></tr></thead>
      <tbody>
        <tr v-for="l in lines" :key="l.id">
          <td>{{ l.name }}</td>
          <td class="num">{{ l.expected }}</td>
          <td class="num"><input v-model.number="l.received" type="number" min="0" :max="l.expected" style="width:80px" /></td>
          <td class="num">
            <span v-if="(l.received ?? 0) === l.expected" class="diff-pos">0</span>
            <span v-else class="diff-neg">{{ (l.received ?? 0) - l.expected }}</span>
          </td>
          <td class="small muted">{{ l.received < l.expected ? '将生成少盒差异' : '' }}</td>
        </tr>
      </tbody>
    </table>

    <h3>错餐登记（非本批次餐品，实物暂扣）</h3>
    <table v-if="wrong.length">
      <thead><tr><th>实际收到餐品</th><th class="num">数量</th><th></th></tr></thead>
      <tbody>
        <tr v-for="(w,i) in wrong" :key="i">
          <td><input v-model="w.name" style="width:200px" placeholder="如：鱼香肉丝饭(错送)" /></td>
          <td class="num"><input v-model.number="w.qty" type="number" min="0" style="width:80px" /></td>
          <td><button class="danger" @click="wrong.splice(i,1)">删</button></td>
        </tr>
      </tbody>
    </table>
    <button class="mt8" @click="wrong.push({name:'',qty:1})">+ 登记错餐</button>

    <div class="grid2 mt12">
      <label class="field">签收人
        <input v-model="receiver" placeholder="前台签收人姓名" />
      </label>
      <label class="field">封签情况
        <select v-model="sealOK"><option :value="true">封签完好</option><option :value="false">封签异常（破损/不符）</option></select>
      </label>
    </div>
    <label class="field mt12">封签/其他说明
      <input v-model="sealNote" placeholder="封签异常时请描述封签号与情况" />
    </label>
    <div class="row end mt12">
      <button class="primary" @click="submit">提交签收并核对数量</button>
    </div>
  </div>
</template>
<script setup>
import { ref } from 'vue'
import { api } from '../../api.js'
import { notify } from '../../store.js'

const props = defineProps({ d: Object })
const emit = defineEmits(['done'])
const lines = ref((props.d.items || []).map((it) => ({
  id: it.id, name: it.name, expected: it.packed_qty, received: it.packed_qty,
})))
const wrong = ref([])
const receiver = ref('')
const sealOK = ref(true)
const sealNote = ref('')

async function submit() {
  if (!receiver.value.trim()) { notify('请填写签收人'); return }
  const payload = {
    receiver_name: receiver.value.trim(),
    seal_ok: sealOK.value,
    seal_note: sealNote.value,
    lines: [
      ...lines.value.map((l) => ({
        batch_item_id: l.id, name: l.name,
        expected_qty: l.expected, received_qty: l.received ?? 0,
      })),
      ...wrong.value.filter((w) => w.name.trim()).map((w) => ({
        name: w.name.trim(), expected_qty: 0, received_qty: w.qty || 0, wrong: true,
      })),
    ],
  }
  try {
    await api.post(`/frontdesk/batches/${props.d.id}/sign`, payload)
    emit('done', '签收完成，差异已自动登记待复核')
  } catch (e) { notify(e.message) }
}
</script>

<template>
  <div>
    <template v-if="d.status === 'draft'">
      <div class="row">
        <button @click="edit = true" class="ghost">编辑装箱</button>
        <button class="primary" @click="sealOpen = true">封箱并登记封签</button>
      </div>
      <div v-if="edit" class="mt12">
        <table>
          <thead><tr><th>餐品</th><th class="num">单价(元)</th><th class="num">装箱数</th><th></th></tr></thead>
          <tbody>
            <tr v-for="(it, i) in rows" :key="i">
              <td><input v-model="it.name" style="width:150px" /></td>
              <td class="num"><input v-model.number="it.price" type="number" min="0" style="width:90px" /></td>
              <td class="num"><input v-model.number="it.qty" type="number" min="0" style="width:80px" /></td>
              <td><button class="danger" @click="rows.splice(i,1)">删</button></td>
            </tr>
          </tbody>
        </table>
        <div class="row mt8">
          <button @click="rows.push({name:'',price:0,qty:0})">+ 增加餐品</button>
          <button class="primary" @click="save">保存装箱</button>
        </div>
        <div class="small muted mt8">补送批次只能按复核批准份数装箱，封箱时会做守恒校验。</div>
      </div>
      <div v-if="sealOpen" class="mt12">
        <label class="field">封签号（逐箱一枚，逗号或换行分隔）
          <textarea v-model="sealText" rows="3" :placeholder="'SEAL-' + d.id + '-A, SEAL-' + d.id + '-B'"></textarea>
        </label>
        <div class="row mt8">
          <button class="primary" @click="doSeal">确认封箱</button>
          <button @click="sealOpen = false">取消</button>
        </div>
      </div>
    </template>
    <div v-else class="small muted">已封箱，装箱数量冻结；如需补餐请沿差异复核发起补送批次。</div>
  </div>
</template>
<script setup>
import { ref } from 'vue'
import { api } from '../../api.js'
import { notify } from '../../store.js'

const props = defineProps({ d: Object })
const emit = defineEmits(['done'])
const edit = ref(false)
const sealOpen = ref(false)
const sealText = ref((props.d.boxes || []).map((b) => b.seal_no).join(', '))
const rows = ref((props.d.items || []).map((it) => ({ name: it.name, price: it.unit_price / 100, qty: it.packed_qty })))

async function save() {
  try {
    await api.put(`/kitchen/batches/${props.d.id}/packing`, {
      items: rows.value
        .filter((r) => r.name.trim())
        .map((r) => ({ name: r.name.trim(), unit_price: Math.round(r.price * 100), packed_qty: r.qty || 0 })),
    })
    emit('done', '装箱清单已保存')
  } catch (e) { notify(e.message) }
}
async function doSeal() {
  const nums = sealText.value.split(/[,，\n]/).map((s) => s.trim()).filter(Boolean)
  try {
    await api.post(`/kitchen/batches/${props.d.id}/seal`, { seal_numbers: nums })
    emit('done', '已封箱，封签登记完成')
  } catch (e) { notify(e.message) }
}
</script>

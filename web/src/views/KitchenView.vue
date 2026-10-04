<template>
  <div>
    <BatchList title="厨房装箱工作台" :refresh-key="refreshKey" @open="$emit('open',$event)">
      <template #actions>
        <button class="primary" @click="show = true">+ 按企业/餐次装箱</button>
      </template>
    </BatchList>

    <Modal v-if="show" @close="show = false">
      <h2>生成装箱清单</h2>
      <div class="muted small">一个企业一个餐次只能有一个原批次；少餐补送沿差异复核发起，不在此另建订单。</div>
      <div class="grid2 mt12">
        <label class="field">企业
          <select v-model="form.enterprise_id">
            <option :value="0" disabled>选择企业…</option>
            <option v-for="e in enterprises" :key="e.id" :value="e.id">{{ e.name }}</option>
          </select>
        </label>
        <div class="row gap8">
          <label class="field">日期<input type="date" v-model="form.meal_date" /></label>
          <label class="field">餐次
            <select v-model="form.meal_type">
              <option value="lunch">午餐</option><option value="breakfast">早餐</option><option value="dinner">晚餐</option>
            </select>
          </label>
        </div>
      </div>
      <table class="mt12">
        <thead><tr><th>餐品</th><th class="num">单价(元)</th><th class="num">数量</th><th></th></tr></thead>
        <tbody>
          <tr v-for="(it,i) in form.items" :key="i">
            <td><input v-model="it.name" style="width:200px" /></td>
            <td class="num"><input v-model.number="it.unit_price" type="number" min="0" style="width:100px" /></td>
            <td class="num"><input v-model.number="it.packed_qty" type="number" min="0" style="width:90px" /></td>
            <td><button class="danger" @click="form.items.splice(i,1)">删</button></td>
          </tr>
        </tbody>
      </table>
      <div class="row mt8">
        <button @click="form.items.push({name:'',unit_price:25,packed_qty:1})">+ 增加餐品</button>
      </div>
      <div class="row end mt12">
        <button @click="show = false">取消</button>
        <button class="primary" @click="submit">生成批次（草稿）</button>
      </div>
    </Modal>
  </div>
</template>
<script setup>
import { ref } from 'vue'
import { api } from '../api.js'
import { store, notify } from '../store.js'
import BatchList from '../components/BatchList.vue'
import Modal from '../components/Modal.vue'

defineEmits(['open'])
const refreshKey = ref(0)
const enterprises = store.enterprises
const show = ref(false)
const today = new Date().toISOString().slice(0, 10)
const form = ref({
  enterprise_id: enterprises[0]?.id || 0,
  meal_date: today, meal_type: 'lunch',
  items: [{ name: '招牌盒饭', unit_price: 25, packed_qty: 10 }],
})

async function submit() {
  try {
    await api.post('/kitchen/batches', {
      ...form.value,
      items: form.value.items
        .filter((i) => i.name.trim())
        .map((i) => ({ name: i.name.trim(), unit_price: Math.round((i.unit_price || 0) * 100), packed_qty: i.packed_qty || 0 })),
    })
    show.value = false
    refreshKey.value++
    notify('装箱批次已创建（草稿），可继续编辑后封箱')
  } catch (e) { notify(e.message) }
}
</script>

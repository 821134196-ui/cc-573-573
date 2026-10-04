<template>
  <div class="row">
    <select v-model="sealNo">
      <option value="" disabled>选择破损封签…</option>
      <option v-for="b in intactBoxes" :key="b.id" :value="b.seal_no">{{ b.code }} / {{ b.seal_no }}</option>
    </select>
    <input v-model="note" placeholder="破损经过说明" style="width:300px" />
    <button class="danger" :disabled="!sealNo || !note" @click="submit">登记封签破损</button>
  </div>
</template>
<script setup>
import { computed, ref } from 'vue'
import { api } from '../../api.js'
import { notify } from '../../store.js'

const props = defineProps({ d: Object })
const emit = defineEmits(['done'])
const intactBoxes = computed(() => (props.d.boxes || []).filter((b) => b.status === 'intact'))
const sealNo = ref('')
const note = ref('')

async function submit() {
  try {
    await api.post(`/batches/${props.d.id}/seal-broken`, { seal_no: sealNo.value, note: note.value })
    emit('done', '封签破损已登记并生成差异单')
  } catch (e) { notify(e.message) }
}
</script>

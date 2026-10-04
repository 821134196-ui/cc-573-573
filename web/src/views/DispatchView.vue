<template>
  <BatchList title="调度工作台（路线指派 / 途中改派）" :refresh-key="refreshKey" @open="$emit('open',$event)" />
</template>
<script setup>
import { ref } from 'vue'
import BatchList from '../components/BatchList.vue'
defineEmits(['open'])
const refreshKey = ref(0)
</script>

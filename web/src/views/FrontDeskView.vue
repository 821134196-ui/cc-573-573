<template>
  <BatchList title="企业前台 · 签收与差异（仅本企业批次）" :refresh-key="refreshKey" @open="$emit('open',$event)" />
</template>
<script setup>
import { ref } from 'vue'
import BatchList from '../components/BatchList.vue'
defineEmits(['open'])
const refreshKey = ref(0)
</script>

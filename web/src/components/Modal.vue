<template>
  <div class="modal-mask" @click.self="$emit('close')">
    <div class="modal" :class="{ wide }">
      <slot />
    </div>
  </div>
</template>
<script setup>
defineProps({ wide: Boolean })
defineEmits(['close'])
</script>

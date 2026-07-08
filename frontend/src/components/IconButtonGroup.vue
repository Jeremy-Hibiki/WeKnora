<template>
  <div
    :class="['icon-btn-group', `icon-btn-group--${align}`]"
    :style="{ gap }"
  >
    <slot />
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  align?: 'center' | 'start' | 'end'
  gap?: string
}>(), {
  align: 'center',
  gap: '4px',
})
</script>

<style lang="less" scoped>
.icon-btn-group {
  display: flex;
  align-items: center;
  flex-shrink: 0;

  &--center {
    justify-content: center;
  }

  &--start {
    justify-content: flex-start;
  }

  &--end {
    justify-content: flex-end;
  }
}
</style>
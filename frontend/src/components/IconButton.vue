<template>
  <t-tooltip
    v-if="tooltip"
    :content="tooltip"
    :placement="placement"
    :disabled="disabled"
  >
    <button
      type="button"
      :class="['icon-btn', iconBtnClass]"
      :disabled="disabled"
      :aria-label="tooltip"
      @click="$emit('click', $event)"
    >
      <t-icon :name="icon" :size="iconSize" />
    </button>
  </t-tooltip>
  <button
    v-else
    type="button"
    :class="['icon-btn', iconBtnClass]"
    :disabled="disabled"
    @click="$emit('click', $event)"
  >
    <t-icon :name="icon" :size="iconSize" />
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  icon: string
  tooltip?: string
  placement?: string
  disabled?: boolean
  badge?: number | string
  iconSize?: string
}>(), {
  icon: '',
  tooltip: '',
  placement: 'top',
  disabled: false,
  badge: undefined,
  iconSize: '16px',
})

defineEmits<{
  click: [event: MouseEvent]
}>()

const iconBtnClass = computed(() => {
  const classes: string[] = []
  if (props.disabled) classes.push('icon-btn--disabled')
  if (props.badge !== undefined) classes.push('icon-btn--badge')
  return classes
})
</script>

<style lang="less" scoped>
.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  color: var(--td-text-color-secondary);
  background: transparent;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  transition: color 0.2s, background-color 0.2s;

  &:hover {
    color: var(--td-brand-color);
    background: var(--td-bg-color-secondarycontainer);
  }

  &:active {
    color: var(--td-brand-color);
  }

  &--disabled {
    color: var(--td-text-color-placeholder);
    cursor: not-allowed;

    &:hover {
      color: var(--td-text-color-placeholder);
      background: transparent;
    }
  }
}
</style>
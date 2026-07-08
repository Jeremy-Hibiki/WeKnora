<template>
  <t-dialog
    v-model:visible="dialogVisible"
    :header="title"
    :footer="false"
    width="460px"
    placement="center"
    :zIndex="5500"
    @closed="handleClosed"
  >
    <!-- Step 1: choose a target KB -->
    <div v-if="step === 'targets'" class="move-body">
      <div v-if="loading" class="move-loading"><t-loading size="small" /> <span>{{ t('knowledgeBase.loading') }}</span></div>
      <div v-else-if="targets.length === 0" class="move-empty">{{ t('knowledgeBase.moveNoTargets') }}</div>
      <div v-else class="move-target-list">
        <div
          v-for="kb in targets"
          :key="kb.id"
          class="move-target-row"
          @click="selectTarget(kb)"
        >
          <t-icon name="root-list" size="16px" />
          <span class="move-target-name">{{ kb.name }}</span>
          <span v-if="kb.knowledge_count !== undefined" class="move-target-count">{{ kb.knowledge_count }}</span>
          <t-icon name="chevron-right" size="14px" class="move-target-arrow" />
        </div>
      </div>
    </div>

    <!-- Step 2: choose mode + confirm -->
    <div v-else-if="step === 'confirm'" class="move-body">
      <div class="move-target-info">
        <t-icon name="arrow-right" size="14px" />
        <span>{{ selectedTargetName }}</span>
      </div>
      <div class="move-mode-item" :class="{ active: mode === 'reuse_vectors' }" @click="mode = 'reuse_vectors'">
        <t-radio :checked="mode === 'reuse_vectors'" />
        <div class="move-mode-text">
          <span class="move-mode-label">{{ t('knowledgeBase.moveModeReuseVectors') }}</span>
          <span class="move-mode-desc">{{ t('knowledgeBase.moveModeReuseVectorsDesc') }}</span>
        </div>
      </div>
      <div class="move-mode-item" :class="{ active: mode === 'reparse' }" @click="mode = 'reparse'">
        <t-radio :checked="mode === 'reparse'" />
        <div class="move-mode-text">
          <span class="move-mode-label">{{ t('knowledgeBase.moveModeReparse') }}</span>
          <span class="move-mode-desc">{{ t('knowledgeBase.moveModeReparseDesc') }}</span>
        </div>
      </div>
      <div class="move-confirm-actions">
        <t-button size="small" variant="outline" @click="step = 'targets'">{{ t('common.back') }}</t-button>
        <t-button size="small" theme="primary" :loading="submitting" @click="handleConfirm">
          {{ t('knowledgeBase.moveConfirm') }}
        </t-button>
      </div>
    </div>
  </t-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { useI18n } from 'vue-i18n';
import { listMoveTargets, moveKnowledge } from '@/api/knowledge-base';

interface TargetKb {
  id: string;
  name: string;
  knowledge_count?: number;
}

const props = defineProps<{
  visible: boolean;
  /** Knowledge IDs to move. */
  knowledgeIds: string[];
  /** Source KB id, used to fetch eligible targets. */
  sourceKbId: string;
}>();

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void;
  (e: 'moved', taskId?: string): void;
}>();

const { t } = useI18n();
const title = computed(() => t('knowledgeBase.moveToKnowledgeBase'));

const dialogVisible = computed({
  get: () => props.visible,
  set: (v) => emit('update:visible', v),
});

const step = ref<'targets' | 'confirm'>('targets');
const loading = ref(false);
const submitting = ref(false);
const targets = ref<TargetKb[]>([]);
const selectedTargetId = ref('');
const selectedTargetName = ref('');
const mode = ref<'reuse_vectors' | 'reparse'>('reuse_vectors');

// Load eligible targets whenever the dialog opens.
watch(
  () => props.visible,
  async (visible) => {
    if (!visible) return;
    step.value = 'targets';
    loading.value = true;
    targets.value = [];
    selectedTargetId.value = '';
    selectedTargetName.value = '';
    try {
      const res = (await listMoveTargets(props.sourceKbId)) as { data?: TargetKb[] };
      targets.value = res.data || [];
    } catch {
      targets.value = [];
    } finally {
      loading.value = false;
    }
  },
);

const selectTarget = (kb: TargetKb) => {
  selectedTargetId.value = kb.id;
  selectedTargetName.value = kb.name;
  step.value = 'confirm';
};

const handleConfirm = async () => {
  if (props.knowledgeIds.length === 0 || !selectedTargetId.value) return;
  submitting.value = true;
  try {
    const res = (await moveKnowledge({
      knowledge_ids: props.knowledgeIds,
      source_kb_id: props.sourceKbId,
      target_kb_id: selectedTargetId.value,
      mode: mode.value,
    })) as { task_id?: string };
    MessagePlugin.success(t('knowledgeBase.moveStarted'));
    emit('moved', res?.task_id);
    dialogVisible.value = false;
  } catch (err) {
    const message = (err as { message?: string })?.message || t('knowledgeBase.moveFailed');
    MessagePlugin.error(message);
  } finally {
    submitting.value = false;
  }
};

const handleClosed = () => {
  step.value = 'targets';
  selectedTargetId.value = '';
  selectedTargetName.value = '';
};
</script>

<style scoped lang="less">
.move-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 4px 0;
}

.move-loading,
.move-empty {
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: center;
  padding: 24px 0;
  color: var(--td-text-color-secondary);
  font-size: 13px;
}

.move-target-list {
  display: flex;
  flex-direction: column;
  max-height: 320px;
  overflow-y: auto;
}

.move-target-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;

  &:hover {
    background: var(--td-bg-color-container-hover);
  }
}

.move-target-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.move-target-count {
  color: var(--td-text-color-secondary);
  font-size: 12px;
}

.move-target-arrow {
  color: var(--td-text-color-placeholder);
}

.move-target-info {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 0;
  font-size: 13px;
  font-weight: 500;
}

.move-mode-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 6px;
  cursor: pointer;

  &.active {
    border-color: var(--td-brand-color);
    background: var(--td-brand-color-light);
  }
}

.move-mode-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.move-mode-label {
  font-size: 13px;
  font-weight: 500;
}

.move-mode-desc {
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.move-confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 4px;
}
</style>

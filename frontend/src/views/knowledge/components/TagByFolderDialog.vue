<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="visible" class="tag-folder-overlay">
        <div class="tag-folder-modal" role="dialog" :aria-label="$t('tagByFolder.title')">
          <button class="close-btn" type="button" :aria-label="$t('general.close')" @click="handleCancel">
            <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path d="M15 5L5 15M5 5L15 15" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
            </svg>
          </button>

          <header class="modal-header">
            <h2 class="modal-title">{{ $t('tagByFolder.title') }}</h2>
            <p v-if="folderName" class="modal-subtitle">{{ folderName }}</p>
          </header>

          <div class="modal-body">
            <!-- Affected count preview -->
            <div class="affected-count-section">
              <t-icon name="info-circle" size="16px" class="info-icon" />
              <span v-if="countLoading" class="affected-count-loading">
                {{ $t('tagByFolder.counting') }}
              </span>
              <span v-else class="affected-count-text">
                {{ $t('tagByFolder.affectedCount', { count: affectedCount }) }}
              </span>
            </div>

            <!-- Action toggle -->
            <div class="action-toggle-section">
              <label class="action-label">{{ $t('tagByFolder.actionLabel') }}</label>
              <div class="action-toggle-buttons">
                <button
                  type="button"
                  class="action-btn"
                  :class="{ active: action === 'add' }"
                  @click="action = 'add'"
                >
                  <t-icon name="add" size="14px" />
                  {{ $t('tagByFolder.addAction') }}
                </button>
                <button
                  type="button"
                  class="action-btn"
                  :class="{ active: action === 'remove' }"
                  @click="action = 'remove'"
                >
                  <t-icon name="minus" size="14px" />
                  {{ $t('tagByFolder.removeAction') }}
                </button>
              </div>
            </div>

            <!-- Recursive toggle -->
            <div class="recursive-toggle-section">
              <label class="recursive-label">{{ $t('tagByFolder.recursiveLabel') }}</label>
              <t-switch v-model="recursive" size="small" />
              <span class="recursive-hint">{{ $t('tagByFolder.recursiveHint') }}</span>
            </div>

            <!-- Tag picker -->
            <div class="tag-picker-section">
              <label class="tag-picker-label">{{ $t('tagByFolder.tagPickerLabel') }}</label>
              <div v-if="selectedTagsList.length > 0" class="selected-tags">
                <button
                  v-for="tag in selectedTagsList"
                  :key="tag.id"
                  type="button"
                  class="selected-tag-chip"
                  @click="toggleTag(tag.id)"
                >
                  {{ tag.name }}
                  <t-icon name="close" size="12px" class="chip-close" />
                </button>
              </div>
              <t-input
                v-model="searchQuery"
                :placeholder="$t('tagByFolder.searchPlaceholder')"
                clearable
                size="small"
                class="tag-search-input"
              />
              <div v-if="availableTagsList.length > 0" class="available-tags">
                <button
                  v-for="tag in availableTagsList"
                  :key="tag.id"
                  type="button"
                  class="available-tag-chip"
                  @click="toggleTag(tag.id)"
                >
                  {{ tag.name }}
                </button>
              </div>
              <div v-else-if="searchQuery && !creatingTag" class="no-tags">
                <span>{{ $t('tagByFolder.noTagsFound') }}</span>
                <button type="button" class="create-tag-btn" @click="handleCreateTag">
                  <t-icon name="add" size="12px" />
                  {{ $t('tagByFolder.createTag', { name: searchQuery }) }}
                </button>
              </div>
            </div>

            <!-- Warning -->
            <div class="warning-section">
              <t-icon name="error-triangle" size="14px" class="warning-icon" />
              <div class="warning-text-group">
                <span class="warning-text">{{ $t('tagByFolder.scopeChangeWarning') }}</span>
                <span class="warning-text hint">{{ $t('tagByFolder.pointInTimeWarning') }}</span>
              </div>
            </div>
          </div>

          <footer class="modal-footer">
            <t-button theme="default" variant="outline" @click="handleCancel">
              {{ $t('common.cancel') }}
            </t-button>
            <t-button
              theme="primary"
              :disabled="!canConfirm"
              :loading="loading"
              @click="handleConfirm"
            >
              {{ action === 'add' ? $t('tagByFolder.confirmAdd') : $t('tagByFolder.confirmRemove') }}
            </t-button>
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { useI18n } from 'vue-i18n';
import { countKnowledgeByFolderIDs } from '@/api/knowledge-folder';
import { createKnowledgeBaseTag } from '@/api/knowledge-base';

interface Tag {
  id: string;
  name: string;
  color?: string;
}

const props = defineProps<{
  visible: boolean;
  kbId: string;
  folderId: string;
  folderName?: string;
  tagList: Tag[];
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  confirm: [payload: { tagIds: string[]; action: 'add' | 'remove'; recursive: boolean }];
  cancel: [];
  'tag-created': [];
}>();

const { t } = useI18n();

const selectedTagIds = ref<Set<string>>(new Set());
const searchQuery = ref('');
const action = ref<'add' | 'remove'>('add');
const recursive = ref(true);
const affectedCount = ref(0);
const countLoading = ref(false);
const creatingTag = ref(false);

const loading = defineModel<boolean>('loading', { default: false });

const selectedTagsList = computed(() =>
  props.tagList.filter((tag) => selectedTagIds.value.has(tag.id)),
);

const availableTagsList = computed(() => {
  const query = searchQuery.value.toLowerCase().trim();
  return props.tagList.filter((tag) => {
    if (selectedTagIds.value.has(tag.id)) return false;
    if (query && !tag.name.toLowerCase().includes(query)) return false;
    return true;
  });
});

const canConfirm = computed(() => selectedTagIds.value.size > 0 && !countLoading.value);

function toggleTag(tagId: string) {
  const next = new Set(selectedTagIds.value);
  if (next.has(tagId)) {
    next.delete(tagId);
  } else {
    next.add(tagId);
  }
  selectedTagIds.value = next;
}

async function fetchCount() {
  if (!props.folderId) return;
  countLoading.value = true;
  try {
    const res = await countKnowledgeByFolderIDs(props.kbId, [props.folderId], recursive.value);
    affectedCount.value = (res as any)?.data?.count ?? 0;
  } catch {
    affectedCount.value = 0;
  } finally {
    countLoading.value = false;
  }
}

async function handleCreateTag() {
  const name = searchQuery.value.trim();
  if (!name || creatingTag.value) return;
  creatingTag.value = true;
  try {
    const res = await createKnowledgeBaseTag(props.kbId, { name });
    const newTag = (res as any)?.data;
    if (newTag) {
      const next = new Set(selectedTagIds.value);
      next.add(String(newTag.id));
      selectedTagIds.value = next;
      searchQuery.value = '';
      MessagePlugin.success(t('knowledgeBase.tagCreateSuccess'));
      emit('tag-created'); // Parent reloads tagList, dialog stays open
    }
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('common.operationFailed'));
  } finally {
    creatingTag.value = false;
  }
}

function handleConfirm() {
  emit('confirm', {
    tagIds: [...selectedTagIds.value],
    action: action.value,
    recursive: recursive.value,
  });
  // Visibility is controlled by the parent (close on success, keep open on failure)
}

function handleCancel() {
  emit('cancel');
  emit('update:visible', false);
}

watch(
  () => props.visible,
  (val) => {
    if (val) {
      selectedTagIds.value = new Set();
      searchQuery.value = '';
      action.value = 'add';
      recursive.value = true;
      fetchCount();
    }
  },
);

watch(recursive, () => {
  if (props.visible) fetchCount();
});
</script>

<style lang="less" scoped>
.tag-folder-overlay {
  position: fixed;
  inset: 0;
  z-index: 3000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.45);
}

.tag-folder-modal {
  position: relative;
  width: 480px;
  max-height: 85vh;
  display: flex;
  flex-direction: column;
  background: var(--td-bg-color-container);
  border-radius: 8px;
  box-shadow: var(--td-shadow-3);
}

.close-btn {
  position: absolute;
  top: 12px;
  right: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  background: transparent;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  border-radius: 4px;

  &:hover {
    background: var(--td-bg-color-secondarycontainer);
    color: var(--td-text-color-primary);
  }
}

.modal-header {
  padding: 20px 24px 8px;
}

.modal-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.modal-subtitle {
  margin: 4px 0 0;
  font-size: 13px;
  color: var(--td-text-color-secondary);
}

.modal-body {
  flex: 1;
  overflow-y: auto;
  padding: 16px 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.affected-count-section {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  background: var(--td-brand-color-light);
  border-radius: 6px;

  .info-icon {
    color: var(--td-brand-color);
    flex-shrink: 0;
  }

  .affected-count-loading,
  .affected-count-text {
    font-size: 13px;
    color: var(--td-text-color-primary);
  }
}

.action-toggle-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.action-label,
.recursive-label,
.tag-picker-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.action-toggle-buttons {
  display: flex;
  gap: 8px;
}

.action-btn {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px 14px;
  border: 1px solid var(--td-component-border);
  border-radius: 4px;
  background: transparent;
  color: var(--td-text-color-secondary);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;

  &:hover {
    border-color: var(--td-brand-color);
    color: var(--td-brand-color);
  }

  &.active {
    border-color: var(--td-brand-color);
    background: var(--td-brand-color-light);
    color: var(--td-brand-color);
  }
}

.recursive-toggle-section {
  display: flex;
  align-items: center;
  gap: 8px;

  .recursive-hint {
    font-size: 12px;
    color: var(--td-text-color-secondary);
  }
}

.tag-picker-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.selected-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.selected-tag-chip {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  border: 1px solid var(--td-brand-color);
  border-radius: 12px;
  background: var(--td-brand-color-light);
  color: var(--td-brand-color);
  font-size: 12px;
  cursor: pointer;

  .chip-close {
    opacity: 0.7;
  }
}

.tag-search-input {
  width: 100%;
}

.available-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-height: 120px;
  overflow-y: auto;
}

.available-tag-chip {
  padding: 3px 8px;
  border: 1px solid var(--td-component-border);
  border-radius: 12px;
  background: transparent;
  color: var(--td-text-color-secondary);
  font-size: 12px;
  cursor: pointer;

  &:hover {
    border-color: var(--td-brand-color);
    color: var(--td-brand-color);
  }
}

.no-tags {
  display: flex;
  flex-direction: column;
  gap: 8px;
  font-size: 12px;
  color: var(--td-text-color-placeholder);

  .create-tag-btn {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 4px 8px;
    border: 1px dashed var(--td-component-border);
    border-radius: 4px;
    background: transparent;
    color: var(--td-brand-color);
    cursor: pointer;
    font-size: 12px;
    align-self: flex-start;
  }
}

.warning-section {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  padding: 10px 12px;
  background: var(--td-warning-color-light);
  border-radius: 6px;

  .warning-icon {
    color: var(--td-warning-color);
    flex-shrink: 0;
    margin-top: 1px;
  }

  .warning-text-group {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .warning-text {
    font-size: 12px;
    color: var(--td-text-color-secondary);
    line-height: 1.5;

    &.hint {
      color: var(--td-text-color-placeholder);
    }
  }
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 24px 20px;
  border-top: 1px solid var(--td-component-stroke);
}
</style>

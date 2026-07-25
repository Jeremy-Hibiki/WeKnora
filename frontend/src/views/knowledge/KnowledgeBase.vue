<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, reactive, computed, nextTick } from "vue";
import { MessagePlugin, DialogPlugin } from "tdesign-vue-next";
import DocContent from "@/components/doc-content.vue";
import useKnowledgeBase from '@/hooks/useKnowledgeBase';
import { useRoute, useRouter } from 'vue-router';
import EmptyKnowledge from '@/components/empty-knowledge.vue';
import ContextualGuide from '@/components/ContextualGuide.vue';
import KBInfoPopover from '@/components/KBInfoPopover.vue';
import KBSwitcherDropdown from '@/components/KBSwitcherDropdown.vue';
import { getSessionsList, createSessions, generateSessionsTitle } from "@/api/chat/index";
import { useMenuStore } from '@/stores/menu';
import { useUIStore } from '@/stores/ui';
import { useOrganizationStore } from '@/stores/organization';
import { useAuthStore } from '@/stores/auth';
import { useChatResourcesStore } from '@/stores/chatResources';
import { useEditorResourcesStore } from '@/stores/editorResources';
import KnowledgeBaseEditorModal from './KnowledgeBaseEditorModal.vue';
const usemenuStore = useMenuStore();
const uiStore = useUIStore();
const orgStore = useOrganizationStore();
const authStore = useAuthStore();
const chatResources = useChatResourcesStore();
const editorResources = useEditorResourcesStore();
const router = useRouter();
import {
  batchQueryKnowledge,
  listKnowledgeTags,
  updateKnowledgeTagBatch,
  uploadKnowledgeFile,
  uploadKnowledgeFolder,
  uploadKnowledgeZip,
  createKnowledgeFromURL,
  reparseKnowledge,
  cancelKnowledgeParse,
  batchDeleteKnowledge,
  batchReparseKnowledge,
  getKnowledgeSpans,
  getKnowledgeDetails,
  type FolderUploadResult,
} from "@/api/knowledge-base/index";
import { createFolder, tagByFolder, countKnowledgeByFolderIDs } from "@/api/knowledge-folder";
import { knowledgeSpansPayloadHasTrace } from '@/utils/knowledgeTrace';
import FAQEntryManager from './components/FAQEntryManager.vue';
import DocumentListView from './components/DocumentListView.vue';
import DocumentCardView from './components/DocumentCardView.vue';
import DocumentBatchBar from './components/DocumentBatchBar.vue';
import IconButton from '@/components/IconButton.vue';
import IconButtonGroup from '@/components/IconButtonGroup.vue';
import KbUploadSourceDropdown from './components/KbUploadSourceDropdown.vue';
import TagEditDialog from './components/TagEditDialog.vue';
import TagByFolderDialog from './components/TagByFolderDialog.vue';
import KbTagManageDrawer from './components/KbTagManageDrawer.vue';
import type { KnowledgeProcessOverrides } from '@/types/knowledgeProcess';
import { useUploadConfirmStore, type UploadConfirmResult } from '@/stores/uploadConfirm';
import WikiBrowser from './wiki/WikiBrowser.vue';
import { getWikiStats } from '@/api/wiki';
import {
  isKnowledgeParseInFlight,
  knowledgeNeedsStatusPolling,
  shouldRefreshWikiStatusAfterKnowledgePoll,
} from './wikiStatusRefresh';
import { getKnowledgeMoveProgress } from '@/api/knowledge-base';
import { batchMoveKnowledgeToFolder, moveFolder } from '@/api/knowledge-folder';
import FolderManageDialog from '@/views/knowledge/components/FolderManageDialog.vue';
import MoveKnowledgeDialog from '@/views/knowledge/components/MoveKnowledgeDialog.vue';
import FolderSelector from '@/views/knowledge/components/FolderSelector.vue';
import { useKnowledgeFolder } from '@/composables/useKnowledgeFolder';
import { getFolderDescendantIds } from '@/utils/knowledgeFolder';
import type { KnowledgeFolder } from '@/types/knowledgeFolder';
import { useI18n } from 'vue-i18n';
import { useMarqueeSelect } from '@/hooks/useMarqueeSelect';
import type { ParserEngineInfo } from '@/api/system';
const route = useRoute();
const { t } = useI18n();
const kbId = computed(() => (route.params as any).kbId as string || '');
const kbInfo = ref<any>(null);
const uploadSourceRef = ref<InstanceType<typeof KbUploadSourceDropdown> | null>(null);
const uploading = ref(false);
const kbLoading = ref(false);
const docListLoading = ref(true);
const isFAQ = computed(() => (kbInfo.value?.type || '') === 'faq');
const isWiki = computed(() => !!kbInfo.value?.indexing_strategy?.wiki_enabled);
const validTabs = ['documents', 'wiki', 'graph'] as const
type KbTab = typeof validTabs[number]
const initTab = validTabs.includes(route.query.tab as any) ? (route.query.tab as KbTab) : 'documents'
const activeKbTab = ref<KbTab>(initTab);

// Wiki 状态用于面包屑上的索引中指示。父组件自行拉取，避免依赖 WikiBrowser 挂载状态
// （用户切到"文档" tab 时 WikiBrowser 会卸载，这里仍需持续反映后台索引进度）。
const wikiStatus = ref<{ pendingTasks: number; isActive: boolean; pendingIssues: number }>({
  pendingTasks: 0,
  isActive: false,
  pendingIssues: 0,
})
const wikiIsIndexing = computed(() => wikiStatus.value.isActive || wikiStatus.value.pendingTasks > 0)
const wikiIndexingTip = computed(() => {
  if (!wikiIsIndexing.value) return ''
  return t('knowledgeEditor.wikiBrowser.queueStatus', { count: wikiStatus.value.pendingTasks || 0 })
})
const onWikiStatusChange = (payload: { pendingTasks: number; isActive: boolean; pendingIssues: number }) => {
  wikiStatus.value = payload
}
const onViewWikiInGraph = async (slug: string) => {
  // Write tab+slug first so the activeKbTab watcher's later replace
  // (which spreads route.query) preserves slug instead of clobbering it.
  await router.replace({ query: { ...route.query, tab: 'graph', slug } })
  activeKbTab.value = 'graph'
}

let wikiStatusTimer: ReturnType<typeof setInterval> | null = null
let wikiStatusProbeTimers: Array<ReturnType<typeof setTimeout>> = []
const stopWikiStatusPolling = () => {
  if (wikiStatusTimer) {
    clearInterval(wikiStatusTimer)
    wikiStatusTimer = null
  }
}
const clearWikiStatusProbes = () => {
  wikiStatusProbeTimers.forEach(t => clearTimeout(t))
  wikiStatusProbeTimers = []
}
const fetchWikiStatusOnce = async () => {
  if (!kbId.value || !isWiki.value) return
  try {
    const res: any = await getWikiStats(kbId.value)
    const data = res?.data || res
    if (!data) return
    wikiStatus.value = {
      pendingTasks: data.pending_tasks || 0,
      isActive: !!data.is_active,
      pendingIssues: data.pending_issues || 0,
    }
    // 活跃时轮询，空闲时停掉定时器，避免无谓请求
    if (wikiIsIndexing.value) {
      if (!wikiStatusTimer) {
        wikiStatusTimer = setInterval(fetchWikiStatusOnce, 5000)
      }
    } else {
      stopWikiStatusPolling()
    }
  } catch (_) { /* ignore */ }
}
// 用户刚触发了一个上传 / reparse / URL 导入之类的动作后，后台通常要过
// 一小段时间才会把 wiki 任务真正塞进队列；如果这时空闲轮询刚好停了，
// 面包屑的"索引中"会延迟很久才亮起。所以这里安排几次退避重试，
// 主动把面包屑的 loading 尽快点亮，一旦探测到任务就会走正常的 5s 轮询。
const scheduleWikiStatusProbes = () => {
  if (!kbId.value || !isWiki.value) return
  clearWikiStatusProbes()
  const delays = [500, 2000, 5000, 10000]
  delays.forEach(delay => {
    const timer = setTimeout(() => { fetchWikiStatusOnce() }, delay)
    wikiStatusProbeTimers.push(timer)
  })
}
watch([kbId, isWiki], ([newKbId, newIsWiki]) => {
  stopWikiStatusPolling()
  clearWikiStatusProbes()
  wikiStatus.value = { pendingTasks: 0, isActive: false, pendingIssues: 0 }
  if (newKbId && newIsWiki) {
    fetchWikiStatusOnce()
  }
}, { immediate: true })
onUnmounted(() => {
  stopWikiStatusPolling()
  clearWikiStatusProbes()
})
const missingStorageEngine = computed(() => {
  if (!kbInfo.value || isFAQ.value) return false
  // storage_backend_id is authoritative; storage_provider_config.provider is a
  // compatibility projection for older clients. Either being present means the
  // KB has a bound storage instance and uploads should not be blocked.
  if (kbInfo.value.storage_backend_id) return false
  const spc = kbInfo.value.storage_provider_config
  return !spc || !spc.provider
})
const parserEngines = computed<ParserEngineInfo[]>(() => editorResources.parserEngines);

const supportedFileTypes = computed<Set<string>>(() => {
  const engines = parserEngines.value
  if (!engines.length) return new Set<string>()

  const rules: { file_types: string[]; engine: string }[] =
    kbInfo.value?.chunking_config?.parser_engine_rules || []

  const ruleMap = new Map<string, string>()
  for (const r of rules) {
    for (const ft of r.file_types) ruleMap.set(ft, r.engine)
  }

  const available = new Set<string>()
  const availableEngineNames = new Set(
    engines.filter(e => e.Available !== false).map(e => e.Name)
  )

  for (const engine of engines) {
    for (const ft of engine.FileTypes || []) {
      if (available.has(ft)) continue

      const explicitEngine = ruleMap.get(ft)
      if (explicitEngine) {
        if (availableEngineNames.has(explicitEngine)) available.add(ft)
      } else {
        if (engine.Available !== false) available.add(ft)
      }
    }
  }
  return available
})

const acceptFileTypes = computed(() =>
  [...supportedFileTypes.value].map(t => '.' + t).join(',')
)

const unsupportedFileTypes = computed<string[]>(() => {
  const engines = parserEngines.value
  if (!engines.length) return []

  const allTypes = new Set<string>()
  for (const engine of engines) {
    for (const ft of engine.FileTypes || []) allTypes.add(ft)
  }

  const supported = supportedFileTypes.value
  return [...allTypes].filter(ft => !supported.has(ft)).sort()
})

const goToParserSettings = () => {
  if (kbId.value) {
    uiStore.openKBSettings(kbId.value, 'parser')
  }
}

// Permission control: check if current user owns this KB or has edit/manage permission
//
// "Owner" here is "the original creator of this KB" (PR 5 introduced
// CreatorID). The previous version compared kb.tenant_id to the active
// tenant id, which only answers "is this KB inside our tenant" — that
// is true even for a Viewer in someone else's tenant, so the gate
// silently bypassed every role check below. Now we require an explicit
// creator match, and the role-aware fallbacks below decide whether a
// non-creator may edit / manage.
const isOwner = computed(() => {
  if (!kbInfo.value) return false;
  const creatorId = (kbInfo.value as any).creator_id || '';
  const userId = authStore.user?.id || '';
  // creator_id may be empty for legacy KBs created before PR 5; treat
  // those as tenant-owned so the role gate applies (Admin+ can manage,
  // Viewer cannot).
  if (!creatorId) return false;
  return creatorId === userId;
});

// Current KB's shared record (when accessed via organization share)
const currentSharedKb = computed(() =>
  orgStore.sharedKnowledgeBases.find((s) => s.knowledge_base?.id === kbId.value) ?? null,
);

// Accessed via organization share: when the KB shows up in our
// sharedKnowledgeBases list it means we reached it through a shared space,
// not because we own/manage it in our tenant. In that case the user's local
// tenant role does NOT grant edit/manage — only the share grant does.
// Without this guard a local tenant Admin would see edit/upload entries on
// a read-only shared KB and get 403'd by the backend on click.
//
// Note: tenant_id comparison alone is unreliable — a user can be a member of
// both the source and receiving tenants, and currentTenantId reflects the
// active switcher rather than "how this KB became visible to me". Presence
// in the share list is the authoritative signal.
const isViaShare = computed(() => !!currentSharedKb.value);

// Can edit: when accessed via an organization share, ONLY the share grant
// counts — even if the current user happens to be the original creator of
// the KB. The backend's RBAC middleware authorizes based on the active
// tenant, not on creator_id, so a creator viewing their own KB from a
// different tenant context will be 403'd on write. Otherwise: KB creator
// (any role) or tenant Admin+ in the home tenant.
//
// hasRole('contributor') is intentionally NOT here — being a Contributor
// in a tenant does not by itself grant edit on someone else's KB.
const canEdit = computed(() => {
  if (isViaShare.value) return orgStore.canEditKB(kbId.value, false);
  if (isOwner.value) return true;
  if (authStore.hasRole('admin')) return true;
  return orgStore.canEditKB(kbId.value, false);
});

// Can manage (delete, settings, etc.): same isViaShare-first rule. For
// shared KBs only an 'admin' share grant qualifies — editor/viewer (and
// even being the creator viewed via share) never grant delete/settings.
const canManage = computed(() => {
  if (isViaShare.value) return orgStore.canManageKB(kbId.value, false);
  if (isOwner.value) return true;
  if (authStore.hasRole('admin')) return true;
  return orgStore.canManageKB(kbId.value, false);
});

// The activity feed exposes owner-side actor and configuration summaries.
// It lives in KB settings (KnowledgeBaseEditorModal) for Owner/Admin in the home tenant.

// Can mutate knowledge (move / batch-delete): the backend gate for these
// two endpoints is g.Contributor(), so the caller MUST be Contributor+
// in their tenant on top of having KB edit permission. Without the extra
// role check, an org-share-editor whose tenant role is Viewer would see
// the "Move" / "Batch manage" entries and 403 on click. For shared KBs
// the local tenant role is irrelevant — canEdit already encodes the share
// grant, so trust it.
const canMutateKnowledge = computed(() => {
  if (!canEdit.value) return false;
  if (isViaShare.value) return true;
  if (isOwner.value) return true;
  if (authStore.hasRole('admin')) return true;
  return authStore.hasRole('contributor');
});

// Effective permission: from direct org share list or from GET /knowledge-bases/:id (e.g. agent-visible KB)
const effectiveKBPermission = computed(() => orgStore.getKBPermission(kbId.value) || kbInfo.value?.my_permission || '');

// Downloading returns the original source file, which is intentionally more
// restrictive than viewing parsed content or using the preview tab. A tenant
// Viewer can never download; for cross-tenant KBs the effective share
// permission must additionally be Editor or Admin.
const canDownloadKnowledge = computed(() => {
  if (!authStore.hasRole('contributor')) return false;
  const permission = effectiveKBPermission.value;
  return !permission || permission === 'owner' || permission === 'admin' || permission === 'editor';
});

const knowledgeList = ref<Array<{ id: string; name: string; type?: string }>>([]);
let { cardList, total, moreIndex, details, getKnowled, delKnowledge, openMore, onVisibleChange: _onVisibleChange, getCardDetails, getfDetails, matchedFolderIds } = useKnowledgeBase(kbId.value)

const showKbDetailContextualGuide = computed(() => {
  return Boolean(kbId.value)
    && !isFAQ.value
    && canEdit.value
    && !docListLoading.value
    && cardList.value.length === 0;
});

const onVisibleChange = (visible: boolean) => {
  _onVisibleChange(visible);
};

/** Per-knowledge cache: whether /spans has a real trace (see knowledgeSpansPayloadHasTrace). */
const traceAvailableById = reactive<Record<string, boolean>>({});
const traceProbeInflight = new Set<string>();

function clearTraceAvailabilityCache() {
  for (const key of Object.keys(traceAvailableById)) {
    delete traceAvailableById[key];
  }
  traceProbeInflight.clear();
}

// Parse phases where the backend pipeline is still actively running
// (primary parse OR post-process fan-out). Trace data exists and the
// UI should treat the row as "in flight" rather than terminal.
function isParseInFlight(status?: string): boolean {
  return isKnowledgeParseInFlight(status);
}

function isTraceMenuVisible(item: KnowledgeCard): boolean {
  if (!item?.id) return false;
  if (isParseInFlight(item.parse_status)) {
    return true;
  }
  return traceAvailableById[item.id] === true;
}

async function probeTraceAvailable(item: KnowledgeCard) {
  const id = item.id;
  if (!id || traceProbeInflight.has(id)) return;
  if (isParseInFlight(item.parse_status)) {
    traceAvailableById[id] = true;
    return;
  }
  if (Object.prototype.hasOwnProperty.call(traceAvailableById, id)) return;
  traceProbeInflight.add(id);
  try {
    const res: any = await getKnowledgeSpans(id);
    traceAvailableById[id] = !!(res?.success && knowledgeSpansPayloadHasTrace(res.data));
  } catch {
    traceAvailableById[id] = false;
  } finally {
    traceProbeInflight.delete(id);
  }
}

const onCardMoreVisibleChange = (visible: boolean, item: KnowledgeCard) => {
  onVisibleChange(visible);
  if (visible) {
    probeTraceAvailable(item);
  }
};
let isCardDetails = ref(false);
let timeout: ReturnType<typeof setTimeout> | null = null;
let knowledgeScroll = ref()
let page = 1;
let pageSize = 35;
let scrollLoading = false;
const resetPage = () => { page = 1; scrollLoading = false; };

// Move state — driven by dialogs (MoveKnowledgeDialog for KB move,
// FolderSelector for folder move), so it works in both grid and list views.
const moveKbDialogVisible = ref(false);
const moveKbDialogIds = ref<string[]>([]);
// Move-knowledge-to-folder state
const moveFolderDialogVisible = ref(false);
const moveFolderKnowledgeIds = ref<string[]>([]);
// Move-folder-to-folder state
const folderMoveDialogVisible = ref(false);
const folderToMove = ref<KnowledgeFolder | null>(null);
// Move sub-flow state used by DocumentCardView/DocumentListView inline menus.
const moveMenuMode = ref<'normal' | 'targets' | 'confirm'>('normal');
const moveTargetKbs = ref<any[]>([]);
const moveTargetsLoading = ref(false);
const moveSelectedTargetName = ref('');
const moveMode = ref<'reuse_vectors' | 'reparse'>('reuse_vectors');
const moveSubmitting = ref(false);
// Batch move state: folders + knowledge IDs to move together.
const batchMoveFolders = ref<KnowledgeFolder[]>([]);
const batchMoveKnowledgeIds = ref<string[]>([]);
// Disabled folder IDs for the move-folder dialog.
const folderMoveDisabledIds = computed(() => {
  const sourceIds = folderToMove.value
    ? [folderToMove.value.id]
    : batchMoveFolders.value.map((f) => f.id);
  return getFolderDescendantIds(folderTree.value, sourceIds);
});
let movePollTimer: ReturnType<typeof setInterval> | null = null;

// View mode (grid / list) — persisted per browser
type DocViewMode = 'grid' | 'list';
const VIEW_MODE_KEY = 'weknora.kb.docs.viewMode';
const initViewMode = (): DocViewMode => {
  try {
    return localStorage.getItem(VIEW_MODE_KEY) === 'list' ? 'list' : 'grid';
  } catch { return 'grid'; }
};
const viewMode = ref<DocViewMode>(initViewMode());
watch(viewMode, (v) => {
  try { localStorage.setItem(VIEW_MODE_KEY, v); } catch { /* ignore */ }
});

// Multi-select state — shared between grid and list views.
// Vue 3.5 tracks Set#add/delete natively, so direct mutation is reactive.
const selectedIds = ref<Set<string>>(new Set());
let lastSelectedIndex = -1;
const batchDeleting = ref(false);
const batchReparsing = ref(false);
// IDs submitted for async batch reparse; hold optimistic pending until the worker updates DB.
const pendingReparseAck = ref<Set<string>>(new Set());

const applyOptimisticBatchReparse = (ids: string[]) => {
  const idSet = new Set(ids);
  for (const card of cardList.value) {
    if (!idSet.has(card.id)) continue;
    pendingReparseAck.value.add(card.id);
    card.parse_status = 'pending';
    card.summary_status = undefined;
    card.description = '';
    delete traceAvailableById[card.id];
    traceAvailableById[card.id] = true;
  }
};

const syncReparseAckFromServer = (ids: string[]) => {
  for (const id of ids) {
    if (!pendingReparseAck.value.has(id)) continue;
    const card = cardList.value.find((c) => c.id === id);
    if (card && isParseInFlight(card.parse_status)) {
      pendingReparseAck.value.delete(id);
    }
  }
};

const awaitBatchReparseReflection = async (ids: string[]) => {
  const maxPolls = 30;
  const delayMs = 400;
  for (let i = 0; i < maxPolls && pendingReparseAck.value.size > 0; i++) {
    await loadKnowledgeFiles(kbId.value);
    syncReparseAckFromServer(ids);
    applyOptimisticBatchReparse(Array.from(pendingReparseAck.value));
    await new Promise<void>((r) => setTimeout(r, delayMs));
  }
  pendingReparseAck.value.clear();
};

const confirmBatchReparse = async () => {
  if (batchReparsing.value || batchDeleting.value || selectedIds.value.size === 0) return;
  const allIds = Array.from(selectedIds.value);
  const ids = allIds.filter((id) => {
    const item = cardList.value.find((c) => c.id === id);
    return !item || !isParseInFlight(item.parse_status);
  });
  const skipped = allIds.length - ids.length;
  if (ids.length === 0) {
    MessagePlugin.info(t('knowledgeBase.rebuildInProgress'));
    return;
  }
  if (skipped > 0) {
    MessagePlugin.warning(t('knowledgeBase.batchReparseSkippedInFlight', { count: skipped }));
  }
  batchReparsing.value = true;
  try {
    const res: any = await batchReparseKnowledge(kbId.value, ids);
    if (res?.success) {
      MessagePlugin.success(t('knowledgeBase.batchReparseSuccess', { count: ids.length }));
      applyOptimisticBatchReparse(ids);
      clearSelection();
      batchMode.value = false;
      scheduleWikiStatusProbes();
      void awaitBatchReparseReflection(ids);
    } else {
      MessagePlugin.error(res?.message || t('knowledgeBase.batchReparseFailed'));
    }
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('knowledgeBase.batchReparseFailed'));
  } finally {
    batchReparsing.value = false;
  }
};

const tagFilterPanelVisible = ref(false);
const tagFilterTriggerHover = ref(false);
const tagFilterCleared = ref(false);
const tagManageDrawerVisible = ref(false);

const showTagFilterClear = computed(
  () => selectedTagIds.value.length > 0 && tagFilterTriggerHover.value,
);

const isTagFilterPlaceholder = computed(
  () => selectedTagIds.value.length === 0 && tagFilterCleared.value,
);

const selectedTagIds = ref<string[]>([]);
const tagList = ref<any[]>([]);
const tagLoading = ref(false);
const tagSearchQuery = ref('');
const TAG_PAGE_SIZE = 50;
const tagPage = ref(1);
const tagHasMore = ref(false);
const tagLoadingMore = ref(false);
const tagTotal = ref(0);
let tagSearchDebounce: number | null = null;
let docSearchDebounce: number | null = null;
const docSearchKeyword = ref('');
// When true, the document keyword search is scoped to the current folder
// (sends folder_scope = currentFolderId). Only meaningful while browsing
// inside a folder; the toggle survives folder navigation within the same
// knowledge base and resets when switching knowledge bases.
const searchInFolder = ref(false);
const selectedFileType = ref('');
const fileTypeOptions = computed(() => [
  { label: t('knowledgeBase.allFileTypes'), value: '' },
  { label: 'PDF', value: 'pdf' },
  { label: 'DOCX', value: 'docx' },
  { label: 'DOC', value: 'doc' },
  { label: 'PPTX', value: 'pptx' },
  { label: 'PPT', value: 'ppt' },
  { label: 'EPUB', value: 'epub' },
  { label: 'MHTML', value: 'mhtml' },
  { label: 'TXT', value: 'txt' },
  { label: 'MD', value: 'md' },
  { label: 'URL', value: 'url' },
  { label: t('knowledgeBase.typeManual'), value: 'manual' },
  { label: 'MP3', value: 'mp3' },
  { label: 'WAV', value: 'wav' },
  { label: 'M4A', value: 'm4a' },
  { label: 'FLAC', value: 'flac' },
  { label: 'OGG', value: 'ogg' },
]);
const selectedParseStatus = ref('');
const parseStatusOptions = computed(() => [
  { label: t('knowledgeBase.allParseStatuses'), value: '' },
  { label: t('knowledgeBase.parseStatusPending'), value: 'pending' },
  { label: t('knowledgeBase.parseStatusProcessing'), value: 'processing' },
  { label: t('knowledgeBase.parseStatusCompleted'), value: 'completed' },
  { label: t('knowledgeBase.parseStatusFailed'), value: 'failed' },
  { label: t('knowledgeBase.parseStatusCancelled'), value: 'cancelled' },
  { label: t('knowledgeBase.parseStatusFinalizing'), value: 'finalizing' },
  { label: t('knowledgeBase.parseStatusDraft'), value: 'draft' },
]);
const selectedSource = ref('');
// Source filter combines ingestion channels and the "manual"/"url" virtual
// sources that the backend routes onto the `type` column.
const sourceOptions = computed(() => [
  { label: t('knowledgeBase.allSources'), value: '' },
  { label: t('knowledgeBase.sourceUpload'), value: 'web' },
  { label: t('knowledgeBase.sourceUrl'), value: 'url' },
  { label: t('knowledgeBase.sourceManual'), value: 'manual' },
  { label: t('knowledgeBase.sourceApi'), value: 'api' },
  { label: t('knowledgeBase.sourceBrowserExtension'), value: 'browser_extension' },
  { label: t('knowledgeBase.channelFeishu'), value: 'feishu' },
  { label: t('knowledgeBase.channelNotion'), value: 'notion' },
  { label: t('knowledgeBase.channelYuque'), value: 'yuque' },
  { label: t('knowledgeBase.channelWechat'), value: 'wechat' },
  { label: t('knowledgeBase.channelWecom'), value: 'wecom' },
  { label: t('knowledgeBase.channelDingtalk'), value: 'dingtalk' },
  { label: t('knowledgeBase.channelSlack'), value: 'slack' },
  { label: t('knowledgeBase.channelIm'), value: 'im' },
]);
// Date range as [start, end] in "YYYY-MM-DD" form (t-date-range-picker default).
const updatedTimeRange = ref<string[]>([]);
// Disable any date after today so users cannot filter into the future.
const disableFutureDate = { after: new Date(new Date().setHours(23, 59, 59, 999)) };
// Folder state
const {
  currentFolderId,
  folders,
  breadcrumbPath,
  folderTree,
  foldersLoading,
  treeLoading,
  currentFolderPath,
  navigateToFolder,
  loadFolderTree,
  loadFolders,
  handleDeleteFolder,
} = useKnowledgeFolder(kbId);

// Folder management dialog state
const folderDialogVisible = ref(false);
const folderDialogMode = ref<'create' | 'edit'>('create');
const currentEditFolder = ref<KnowledgeFolder | null>(null);

// Load folders when kbId or currentFolderId changes
watch([kbId, currentFolderId], ([newKbId, newFolderId]) => {
  if (newKbId) {
    loadFolders(newFolderId);
    loadFolderTree();
  }
}, { immediate: true });

// Reload files when folder changes. The 'search in this folder' toggle is
// intentionally kept across folder navigation so users can walk the tree
// while the scoped search stays active.
watch(currentFolderId, () => {
  resetPage();
  loadKnowledgeFiles(kbId.value);
});

// Switching knowledge bases is a fresh context: reset the scoped-search toggle.
watch(kbId, () => {
  searchInFolder.value = false;
});

const handleFolderNavigate = async (folderId: string | null) => {
  await navigateToFolder(folderId);
};

const handleCreateFolder = () => {
  folderDialogMode.value = 'create';
  currentEditFolder.value = null;
  folderDialogVisible.value = true;
};
const handleRenameFolder = (folder: KnowledgeFolder) => {
  folderDialogMode.value = 'edit';
  currentEditFolder.value = folder;
  folderDialogVisible.value = true;
};

const confirmDeleteFolder = (folder: KnowledgeFolder) => {
  const name = folder.name || '';
  const count = folder.knowledge_count || 0;
  const msg = count > 0
    ? t('knowledgeFolder.confirmDeleteFolderWithContent', { name, count })
    : t('knowledgeFolder.confirmDeleteFolder', { name });
  const dialog = DialogPlugin.confirm({
    header: t('knowledgeFolder.deleteFolder'),
    body: msg,
    confirmBtn: { content: t('common.confirm'), theme: 'danger' },
    cancelBtn: t('common.cancel'),
    onConfirm: async () => {
      dialog.update({ confirmLoading: true });
      const ok = await handleDeleteFolder(folder.id, count > 0);
      dialog.destroy();
      if (ok) {
        loadKnowledgeFiles(kbId.value);
        loadFolders(currentFolderId.value);
        loadFolderTree();
      }
    },
  });
};

const handleFolderDialogSuccess = () => {
  loadFolders(currentFolderId.value);
  loadFolderTree();
};

// True while the folder-scoped keyword search is active: the backend keeps the
// document list at the current level and returns matched_folder_ids so the
// hierarchy view can hide branches without any hit.
const isFolderScopeSearchActive = computed(() =>
  Boolean(searchInFolder.value && currentFolderId.value && docSearchKeyword.value.trim()),
);

// Paths of the folders (inside the scope subtree) that contain hit documents,
// resolved from the full folder tree. Folder paths are materialized
// (parent.path + id + '/'), so a path-prefix match is exactly an ancestor
// relationship — the same semantics the backend uses for subtree expansion.
const matchedFolderPaths = computed(() => {
  const paths = new Set<string>();
  if (!isFolderScopeSearchActive.value || matchedFolderIds.value.length === 0) {
    return paths;
  }
  const pathById = new Map<string, string>();
  const collectPaths = (nodes: KnowledgeFolder[]) => {
    for (const node of nodes) {
      pathById.set(node.id, node.path);
      if (node.children?.length) collectPaths(node.children);
    }
  };
  collectPaths(folderTree.value);
  for (const id of matchedFolderIds.value) {
    const path = pathById.get(id);
    if (path) paths.add(path);
  }
  return paths;
});

// A child folder stays visible during a scoped search only when its own
// subtree contains at least one hit document.
const folderSubtreeHasMatch = (folder: KnowledgeFolder): boolean => {
  for (const matchedPath of matchedFolderPaths.value) {
    if (matchedPath.startsWith(folder.path)) return true;
  }
  return false;
};

// Prepend folder items to cardList for display
const folderCardItems = computed(() => {
  const visibleFolders = isFolderScopeSearchActive.value
    ? folders.value.filter((f) => folderSubtreeHasMatch(f))
    : folders.value;
  return visibleFolders.map((f) => ({
    ...f,
    id: f.id,
    file_name: f.name,
    type: 'folder',
    isFolder: true,
    knowledge_count: f.knowledge_count || 0,
    created_at: f.created_at,
    updated_at: f.updated_at || f.created_at,
  }));
});

const displayCardList = computed(() => {
  return [...folderCardItems.value, ...cardList.value];
});

const filterParams = computed(() => {
  const [start, end] = updatedTimeRange.value || [];
  const inFolder = currentFolderId.value;
  return {
    tag_ids: selectedTagIds.value.length > 0 ? selectedTagIds.value.join(',') : undefined,
    keyword: docSearchKeyword.value ? docSearchKeyword.value.trim() : undefined,
    file_type: selectedFileType.value || undefined,
    parse_status: selectedParseStatus.value || undefined,
    source: selectedSource.value || undefined,
    start_time: start ? `${start} 00:00:00` : undefined,
    end_time: end ? `${end} 23:59:59` : undefined,
    folder_id: inFolder || '__root__',
    // folder_scope is only sent when the user opts in to searching within the
    // current folder AND has typed a keyword; the backend then keeps the
    // document list at this level and returns matched_folder_ids so the
    // hierarchy view can hide branches without hits. Without a keyword the
    // view must look exactly like the unchecked state.
    folder_scope: isFolderScopeSearchActive.value && inFolder ? inFolder : undefined,
  };
});
const tagMap = computed<Record<string, any>>(() => {
  const map: Record<string, any> = {};
  tagList.value.forEach((tag) => {
    map[tag.id] = tag;
  });
  return map;
});
const sidebarCategoryCount = computed(() => tagTotal.value || tagList.value.length);
const sidebarTags = computed(() => {
  const list = tagList.value;
  const selectedIds = selectedTagIds.value;
  if (selectedIds.length === 0) {
    return list;
  }
  const missing = selectedIds
    .filter((id) => !list.some((tag) => tag.id === id))
    .map((id) => tagMap.value[id])
    .filter(Boolean);
  if (missing.length === 0) {
    return list;
  }
  return [...missing, ...list];
});

const activeTagFilterLabel = computed(() => {
  if (selectedTagIds.value.length === 0) {
    return tagFilterCleared.value
      ? t('knowledgeBase.tagFilterPlaceholder')
      : t('knowledgeBase.allTags');
  }
  if (selectedTagIds.value.length === 1) {
    const id = selectedTagIds.value[0];
    return tagMap.value[id]?.name || t('knowledgeBase.allTags');
  }
  return t('knowledgeBase.tagFilterMulti', { count: selectedTagIds.value.length });
});

const activeTagFilterTitle = computed(() => {
  if (selectedTagIds.value.length === 0) {
    return t('knowledgeBase.tagFilterTitle');
  }
  const names = selectedTagIds.value
    .map((id) => tagMap.value[id]?.name)
    .filter(Boolean);
  return names.length > 0 ? names.join('、') : t('knowledgeBase.tagFilterTitle');
});

const isTagFilterActive = (tagId: string) => selectedTagIds.value.includes(tagId);

// 标签编辑弹窗
const tagEditDialogVisible = ref(false);
const tagEditTarget = ref<KnowledgeCard | null>(null);
const batchTagDialogVisible = ref(false);
const batchTagLoading = ref(false);
const batchTagInitialTags = ref<Array<{ id: string; name: string; color?: string }>>([]);

// Tag-by-folder dialog state
const tagByFolderDialogVisible = ref(false);
const tagByFolderTarget = ref<{ id: string; name: string } | null>(null);
const tagByFolderLoading = ref(false);

function openTagByFolderDialog(folder: { id: string; file_name?: string; name?: string }) {
  tagByFolderTarget.value = { id: folder.id, name: folder.file_name || folder.name || '' };
  tagByFolderDialogVisible.value = true;
}

async function confirmTagByFolder(payload: { tagIds: string[]; action: 'add' | 'remove'; recursive: boolean }) {
  if (tagByFolderLoading.value || !tagByFolderTarget.value) return;
  tagByFolderLoading.value = true;
  try {
    await tagByFolder(kbId.value, {
      folder_ids: [tagByFolderTarget.value.id],
      tag_ids: payload.tagIds,
      action: payload.action,
      recursive: payload.recursive,
    });
    MessagePlugin.success(t('tagByFolder.success'));
    tagByFolderDialogVisible.value = false;
    loadKnowledgeFiles(kbId.value);
    loadTags(kbId.value, true);
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('common.operationFailed'));
  } finally {
    tagByFolderLoading.value = false;
  }
}

function openTagEditDialog(item: KnowledgeCard) {
  tagEditTarget.value = item;
  tagEditDialogVisible.value = true;
}

function onTagEditConfirm(tagIds: string[]) {
  if (tagEditTarget.value) {
    handleKnowledgeTagChange(tagEditTarget.value.id, tagIds);
  }
}

function openBatchTagDialog() {
  type Tag = { id: string; name: string; color?: string };
  // 计算所有选中文档的标签交集
  const selectedCards: KnowledgeCard[] = cardList.value.filter((c: KnowledgeCard) => selectedIds.value.has(c.id));
  if (selectedCards.length === 0) return;
  let commonTags: Array<Tag> | null = null;
  for (const card of selectedCards) {
    const cardTags = card.tags || [];
    if (commonTags === null) {
      commonTags = [...cardTags];
    } else {
      const commonMap: Map<string, Tag> = new Map(commonTags.map((t) => [t.id, t]));
      const cardTagIds = new Set(cardTags.map((t) => t.id));
      for (const t of commonTags) {
        if (!cardTagIds.has(t.id)) commonMap.delete(t.id);
      }
      commonTags = Array.from(commonMap.values());
    }
  }
  batchTagInitialTags.value = commonTags || [];
  batchTagDialogVisible.value = true;
}

async function confirmBatchTag(tagIds: string[]) {
  if (batchTagLoading.value || selectedIds.value.size === 0) return;
  batchTagLoading.value = true;
  try {
    const ids = Array.from(selectedIds.value);
    // 追加模式：保留各文档原有标签，合并本次选择的标签
    const updates: Record<string, string[]> = {};
    for (const id of ids) {
      const card = cardList.value.find((c: KnowledgeCard) => c.id === id);
      const existingTagIds = (card?.tags || []).map((t: any) => t.id);
      // 合并去重：先保留原有标签，再追加新选的
      const merged = new Set([...existingTagIds, ...tagIds]);
      updates[id] = Array.from(merged);
    }
    await updateKnowledgeTagBatch({ updates });
    MessagePlugin.success(t('knowledgeBase.tagUpdateSuccess'));
    batchTagDialogVisible.value = false;
    clearSelection();
    batchMode.value = false;
    loadKnowledgeFiles(kbId.value);
    loadTags(kbId.value, true);
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('common.operationFailed'));
  } finally {
    batchTagLoading.value = false;
  }
}
const getPageSize = () => {
  const viewportHeight = window.innerHeight || document.documentElement.clientHeight;
  const itemHeight = 148;
  let itemsInView = Math.floor(viewportHeight / itemHeight) * 5;
  pageSize = Math.max(35, itemsInView);
}
getPageSize()
// 直接调用 API 获取知识库文件列表
const getTagName = (tagId?: string | number) => {
  if (!tagId && tagId !== 0) return '';
  const key = String(tagId);
  return tagMap.value[key]?.name || '';
};

const loadKnowledgeFiles = (kbIdValue: string): Promise<void> => {
  if (!kbIdValue) return Promise.resolve();
  if (!isFAQ.value) {
    docListLoading.value = true;
  }
  return getKnowled(
    {
      page: 1,
      page_size: pageSize,
      ...filterParams.value,
    },
    kbIdValue,
  ).finally(() => {
    if (isCurrentKb(kbIdValue) && !isFAQ.value) {
      docListLoading.value = false;
    }
  });
};

const isCurrentKb = (targetKbId: string) => targetKbId === kbId.value;

const loadTags = async (kbIdValue: string, reset = false) => {
  if (!kbIdValue) {
    tagList.value = [];
    tagTotal.value = 0;
    tagHasMore.value = false;
    tagPage.value = 1;
    return;
  }

  if (reset) {
    tagPage.value = 1;
    tagList.value = [];
    tagTotal.value = 0;
    tagHasMore.value = false;
  } else if (tagLoading.value || tagLoadingMore.value) {
    return;
  }

  const currentPage = tagPage.value || 1;
  tagLoading.value = currentPage === 1;
  tagLoadingMore.value = currentPage > 1;

  try {
    const res: any = await listKnowledgeTags(kbIdValue, {
      page: currentPage,
      page_size: TAG_PAGE_SIZE,
      keyword: tagSearchQuery.value || undefined,
    });
    if (!isCurrentKb(kbIdValue)) return;

    const pageData = (res?.data || {}) as {
      data?: any[];
      total?: number;
    };
    const pageTags = (pageData.data || []).map((tag: any) => ({
      ...tag,
      id: String(tag.id),
    }));

    if (currentPage === 1) {
      tagList.value = pageTags;
    } else {
      tagList.value = [...tagList.value, ...pageTags];
    }

    tagTotal.value = pageData.total || tagList.value.length;
    tagHasMore.value = tagList.value.length < tagTotal.value;
    if (tagHasMore.value) {
      tagPage.value = currentPage + 1;
    }
  } catch (error) {
    if (!isCurrentKb(kbIdValue)) return;
    console.error('Failed to load tags', error);
  } finally {
    if (isCurrentKb(kbIdValue)) {
      tagLoading.value = false;
      tagLoadingMore.value = false;
    }
  }
};

const handleTagFilterChange = (tagIds: string[]) => {
  selectedTagIds.value = tagIds;
  // 同步更新 store 中的 selectedTagIds，供 menu.vue 上传时使用
  uiStore.clearSelectedTagIds();
  tagIds.forEach(id => uiStore.toggleSelectedTagId(id));
  resetPage();
};

const handleTagRowClick = (tagId: string) => {
  const next = new Set(selectedTagIds.value);
  if (next.has(tagId)) {
    next.delete(tagId);
  } else {
    next.add(tagId);
  }
  if (next.size > 0) {
    tagFilterCleared.value = false;
  }
  handleTagFilterChange([...next]);
};

const clearTagFilter = () => {
  tagFilterCleared.value = true;
  handleTagFilterChange([]);
};

const openTagManageDrawer = () => {
  tagFilterPanelVisible.value = false;
  tagManageDrawerVisible.value = true;
};

const openTagManageFromEditDialog = () => {
  tagEditDialogVisible.value = false;
  tagManageDrawerVisible.value = true;
};

const onTagManageChanged = (payload?: { deletedTagId?: string }) => {
  if (!kbId.value) return;
  void loadTags(kbId.value, true);
  if (payload?.deletedTagId && selectedTagIds.value.includes(payload.deletedTagId)) {
    selectedTagIds.value = [];
    handleTagFilterChange([]);
    resetPage();
    loadKnowledgeFiles(kbId.value);
    return;
  }
  if (payload?.deletedTagId) {
    void (async () => {
      await new Promise((resolve) => setTimeout(resolve, 800));
      if (!kbId.value) return;
      resetPage();
      await loadKnowledgeFiles(kbId.value);
      await loadTags(kbId.value, true);
    })();
    return;
  }
  resetPage();
  loadKnowledgeFiles(kbId.value);
};

const handleKnowledgeTagChange = async (knowledgeId: string, tagIds: string[]) => {
  try {
    await updateKnowledgeTagBatch({ updates: { [knowledgeId]: tagIds } });
    MessagePlugin.success(t('knowledgeBase.tagUpdateSuccess'));
    resetPage(); // Reset page counter to 1 when reloading files after tag change
    loadKnowledgeFiles(kbId.value);
    loadTags(kbId.value, true);
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('common.operationFailed'));
  }
};

const loadKnowledgeBaseInfo = async (targetKbId: string, force = false) => {
  if (!targetKbId) {
    kbInfo.value = null;
    cardList.value = [];
    total.value = 0;
    return;
  }
  kbLoading.value = true;
  try {
    const data = await chatResources.fetchKnowledgeBaseById(targetKbId, force);
    if (!isCurrentKb(targetKbId)) return;

    kbInfo.value = data;
    selectedTagIds.value = [];
    tagFilterCleared.value = false;
    uiStore.clearSelectedTagIds();
    // 重置store中的标签选择状态，避免上传文档时自动带上之前选择的标签
    uiStore.clearSelectedTagIds();
    if (!isFAQ.value) {
      loadKnowledgeFiles(targetKbId);
    } else {
      cardList.value = [];
      total.value = 0;
    }
    loadTags(targetKbId, true);
  } catch (error) {
    if (!isCurrentKb(targetKbId)) return;

    console.error('Failed to load knowledge base info:', error);
    kbInfo.value = null;
    cardList.value = [];
    total.value = 0;
  } finally {
    if (isCurrentKb(targetKbId)) {
      kbLoading.value = false;
    }
  }
};

const loadKnowledgeList = async () => {
  try {
    await chatResources.ensureKnowledgeBases();
    const myKbs = chatResources.rawKnowledgeBases.map((item: any) => ({
      id: String(item.id),
      name: item.name,
      type: item.type || 'document',
    }));

    // Also include shared knowledge bases from orgStore
    const sharedKbs = (orgStore.sharedKnowledgeBases || [])
      .filter(s => s.knowledge_base != null)
      .map(s => ({
        id: String(s.knowledge_base.id),
        name: s.knowledge_base.name,
        type: s.knowledge_base.type || 'document',
      }));

    // Merge and deduplicate by id (my KBs take precedence)
    const myKbIds = new Set(myKbs.map(kb => kb.id));
    const uniqueSharedKbs = sharedKbs.filter(kb => !myKbIds.has(kb.id));

    knowledgeList.value = [...myKbs, ...uniqueSharedKbs];
  } catch (error) {
    console.error('Failed to load knowledge list:', error);
  }
};

// 监听路由参数变化，重新获取知识库内容
// Sync activeKbTab to URL query so it survives page refresh
watch(activeKbTab, (tab) => {
  const query = { ...route.query }
  if (tab === 'documents') {
    delete query.tab
  } else {
    query.tab = tab
  }
  router.replace({ query })
})

watch(() => kbId.value, (newKbId, oldKbId) => {
  if (!newKbId) {
    kbInfo.value = null;
    cardList.value = [];
    total.value = 0;
    return;
  }
  if (newKbId === oldKbId && kbInfo.value) return;

  if (newKbId !== oldKbId) {
    clearTraceAvailabilityCache();
    cardList.value = [];
    total.value = 0;
    docListLoading.value = true;
    resetPage();
    tagSearchQuery.value = '';
    tagPage.value = 1;
    uiStore.clearSelectedTagIds();
  }
  loadKnowledgeBaseInfo(newKbId);
}, { immediate: true });

watch(selectedTagIds, (newVal, oldVal) => {
  if (oldVal === undefined) return;
  if (kbId.value) {
    loadKnowledgeFiles(kbId.value);
  }
}, { deep: true });

watch(tagSearchQuery, (newVal, oldVal) => {
  if (newVal === oldVal) return;
  if (tagSearchDebounce) {
    window.clearTimeout(tagSearchDebounce);
  }
  tagSearchDebounce = window.setTimeout(() => {
    if (kbId.value) {
      loadTags(kbId.value, true);
    }
  }, 300);
});

// 监听文档搜索关键词变化
watch(docSearchKeyword, (newVal, oldVal) => {
  if (newVal === oldVal) return;
  if (docSearchDebounce) {
    window.clearTimeout(docSearchDebounce);
  }
  docSearchDebounce = window.setTimeout(() => {
    if (kbId.value) {
      resetPage();
      loadKnowledgeFiles(kbId.value);
    }
  }, 300);
});

// 监听文件类型筛选变化
watch(selectedFileType, (newVal, oldVal) => {
  if (newVal === oldVal) return;
  if (kbId.value) {
    resetPage();
    loadKnowledgeFiles(kbId.value);
  }
});

// 监听解析状态/来源/更新时间范围筛选变化（与文件类型行为一致）
watch([selectedParseStatus, selectedSource, updatedTimeRange], () => {
  if (kbId.value) {
    resetPage();
    loadKnowledgeFiles(kbId.value);
  }
}, { deep: true });

// 监听文件上传事件
const handleFileUploaded = (event: CustomEvent) => {
  const uploadedKbId = event.detail.kbId;
  console.log('接收到文件上传事件，上传的知识库ID:', uploadedKbId, '当前知识库ID:', kbId.value);
  if (uploadedKbId && uploadedKbId === kbId.value && !isFAQ.value) {
    console.log('匹配当前知识库，开始刷新文件列表');
    // 如果上传的文件属于当前知识库，使用 loadKnowledgeFiles 刷新文件列表
    resetPage(); // Reset page counter when reloading files after upload
    loadKnowledgeFiles(uploadedKbId);
    loadTags(uploadedKbId);
    // 启动几次探测，尽快让面包屑的"索引中"亮起。
    scheduleWikiStatusProbes();
  }
};


// 监听从菜单触发的URL导入事件
const handleOpenURLImportDialog = (event: CustomEvent) => {
  const eventKbId = event.detail.kbId;
  console.log('接收到URL导入对话框打开事件，知识库ID:', eventKbId, '当前知识库ID:', kbId.value);
  if (eventKbId && eventKbId === kbId.value && !isFAQ.value) {
    if (ensureDocumentKbReady()) {
      uploadSourceRef.value?.openUrlDialog();
    }
  }
};

// Auto-open document detail when navigated with ?knowledge_id=xxx.
// Note: this runs both when the KB page mounts with a query param AND when a
// subsequent in-page navigation (e.g. from the global command palette) only
// changes the query without re-mounting the component — in that case kbId is
// the same and cardList may already be populated, so relying solely on the
// cardList watcher misses the trigger.
const pendingKnowledgeId = ref<string | null>(
  (route.query.knowledge_id as string) || null
);

const tryAutoOpenDocument = () => {
  if (!pendingKnowledgeId.value || !cardList.value?.length) return;
  const targetId = pendingKnowledgeId.value;
  pendingKnowledgeId.value = null;
  const card = cardList.value.find((c: KnowledgeCard) => c.id === targetId);
  if (card) {
    nextTick(() => openCardDetails(card));
  } else {
    nextTick(() => {
      openCardDetails({ id: targetId } as KnowledgeCard);
    });
  }
};

// React to later ?knowledge_id= changes on the same KB route (no remount).
watch(
  () => route.query.knowledge_id,
  (newId) => {
    if (typeof newId !== 'string' || !newId) return;
    pendingKnowledgeId.value = newId;
    // cardList is almost always already loaded at this point; if not, the
    // cardList watcher below will pick it up.
    tryAutoOpenDocument();
  },
);

// Dispatched by the global command palette when the user picks a chunk that
// lives in the KB they are already viewing — vue-router dedupes identical
// navigations, so we rely on this event instead of a URL change.
const handleOpenKnowledgeEvent = (e: Event) => {
  const detail = (e as CustomEvent<{ kbId: string; knowledgeId: string }>).detail;
  if (!detail || !detail.knowledgeId) return;
  if (detail.kbId && detail.kbId !== kbId.value) return;
  pendingKnowledgeId.value = detail.knowledgeId;
  tryAutoOpenDocument();
};

const handleFolderDropEvent = async (event: Event) => {
  if (!ensureDocumentKbReady()) return;
  const detail = (event as CustomEvent<{ files: File[] }>).detail;
  const files = detail?.files ?? [];
  if (files.length === 0) return;
  // Route through the upload-confirm dialog so the user can pick process
  // options, then executeFolderUploadBatch handles the batch endpoint.
  openUploadConfirmDialog(files);
};

onMounted(() => {
  loadKnowledgeList();
  editorResources.ensureParserEngines();

  window.addEventListener('knowledgeFileUploaded', handleFileUploaded as EventListener);
  window.addEventListener('openURLImportDialog', handleOpenURLImportDialog as EventListener);
  window.addEventListener('weknora:open-knowledge', handleOpenKnowledgeEvent as EventListener);
  window.addEventListener('weknora:folder-drop', handleFolderDropEvent as EventListener);
});

onUnmounted(() => {
  window.removeEventListener('knowledgeFileUploaded', handleFileUploaded as EventListener);
  window.removeEventListener('openURLImportDialog', handleOpenURLImportDialog as EventListener);
  window.removeEventListener('weknora:open-knowledge', handleOpenKnowledgeEvent as EventListener);
  window.removeEventListener('weknora:folder-drop', handleFolderDropEvent as EventListener);
  stopMovePoll();
  if (timeout !== null) {
    clearTimeout(timeout);
    timeout = null;
  }
});
watch(() => cardList.value, (newValue) => {
  if (isFAQ.value) return;
  docListLoading.value = false;

  // Auto-open document if navigated with ?knowledge_id=xxx
  if (pendingKnowledgeId.value && newValue?.length) {
    tryAutoOpenDocument();
  }

  let analyzeList = [];
  // Filter items that need polling: parsing in progress OR summary generation in progress
  analyzeList = newValue.filter(needsStatusPolling);
  if (timeout !== null) {
    clearTimeout(timeout);
    timeout = null;
  }
  if (analyzeList.length) {
    updateStatus(analyzeList)
  }

}, { deep: true })
type KnowledgeCard = {
  id: string;
  knowledge_base_id?: string;
  parse_status: string;
  summary_status?: string;
  description?: string;
  file_name?: string;
  original_file_name?: string;
  display_name?: string;
  title?: string;
  type?: string;
  updated_at?: string;
  file_type?: string;
  isMore?: boolean;
  metadata?: any;
  error_message?: string;
  tags?: Array<{ id: string; name: string; color?: string }>;
};
// needsStatusPolling decides whether a card row is still "in flight"
// enough that the doc list should keep refreshing it. Keep in sync with
// the backend lifecycle: pending / processing are the primary parse
// phase, finalizing is the post-process fan-out (summary / question /
// graph extract still running), and a `completed` row whose summary
// hasn't landed yet keeps polling so the description fills in.
const needsStatusPolling = (item: KnowledgeCard) => {
  return knowledgeNeedsStatusPolling(item);
};

const updateStatus = (analyzeList: KnowledgeCard[]) => {
  if (timeout !== null) {
    clearTimeout(timeout);
    timeout = null;
  }
  if (!analyzeList.length) return;

  let query = ``;
  for (let i = 0; i < analyzeList.length; i++) {
    query += `ids=${analyzeList[i].id}&`;
  }
  timeout = setTimeout(() => {
    batchQueryKnowledge(query).then((result: any) => {
      let hasChanges = false;
      let shouldRefreshWikiStatus = false;
      if (result.success && result.data) {
        (result.data as KnowledgeCard[]).forEach((item: KnowledgeCard) => {
          const index = cardList.value.findIndex(card => card.id == item.id);
          if (index == -1) return;

          let parseStatus = item.parse_status;
          if (pendingReparseAck.value.has(item.id)) {
            if (isParseInFlight(item.parse_status)) {
              pendingReparseAck.value.delete(item.id);
            } else {
              parseStatus = 'pending';
            }
          }

          if (cardList.value[index].parse_status !== parseStatus ||
            cardList.value[index].summary_status !== item.summary_status ||
            cardList.value[index].description !== item.description) {
            shouldRefreshWikiStatus ||= shouldRefreshWikiStatusAfterKnowledgePoll(
              cardList.value[index],
              { ...item, parse_status: parseStatus },
            );

            // Always update the card data
            cardList.value[index].parse_status = parseStatus;
            cardList.value[index].summary_status = item.summary_status;
            cardList.value[index].description = item.description;
            delete traceAvailableById[item.id];
            hasChanges = true;
          }
        });
      }
      if (shouldRefreshWikiStatus) {
        void fetchWikiStatusOnce();
      }
      // If there are no changes, the watch won't trigger, so we must manually poll again
      // Even if there are changes, we can manually poll again just to be safe.
      // The watch will clear this timeout if it triggers.
      const stillPending = cardList.value.filter(needsStatusPolling);
      if (stillPending.length > 0) {
        updateStatus(stillPending);
      }
    }).catch((_err) => {
      // 错误处理
      const stillPending = cardList.value.filter(needsStatusPolling);
      if (stillPending.length > 0) {
        updateStatus(stillPending);
      }
    });
  }, 1500);
};


// 恢复文档处理状态（用于刷新后恢复）

const closeDoc = () => {
  isCardDetails.value = false;
};
const openCardDetails = (item: KnowledgeCard) => {
  isCardDetails.value = true;
  getCardDetails(item);
};

// Open source document preview from WikiBrowser
const openSourceDoc = (knowledgeId: string) => {
  isCardDetails.value = true;
  getCardDetails({ id: knowledgeId });
};

const closeCardMoreMenu = (index: number) => {
  if (cardList.value?.[index]) {
    cardList.value[index].isMore = false;
  }
  moreIndex.value = -1;
};

const confirmDeleteKnowledge = (index: number, item: KnowledgeCard) => {
  closeCardMoreMenu(index);
  const deletedId = item?.id;
  delKnowledge(index, item, async () => {
    resetPage();
    const maxPolls = 30;
    const delayMs = 400;
    for (let i = 0; i < maxPolls; i++) {
      await loadKnowledgeFiles(kbId.value);
      const stillPresent = (cardList.value || []).some((c: KnowledgeCard) => c.id === deletedId);
      if (!stillPresent) break;
      await new Promise<void>((r) => setTimeout(r, delayMs));
    }
    loadTags(kbId.value, true);
  });
};

const onReparseMenuClick = (index: number, item: KnowledgeCard) => {
  if (isParseInFlight(item.parse_status)) {
    MessagePlugin.info(t('knowledgeBase.rebuildInProgress'));
  }
};

// Open the move-to-KB dialog for a single document. Works for both grid and
// list views because the target selection lives in a dialog, not an inline
// submenu that only the grid popup renders.
const handleMoveKnowledge = (item: KnowledgeCard) => {
  item.isMore = false; // close the grid card popup so it doesn't sit behind the dialog
  moveKbDialogIds.value = [item.id];
  moveKbDialogVisible.value = true;
};

// Open the move-to-folder dialog for a single document.
const handleMoveToFolder = (item: KnowledgeCard) => {
  item.isMore = false;
  moveFolderKnowledgeIds.value = [item.id];
  moveFolderDialogVisible.value = true;
};

// Batch move-to-folder: selected documents and folders in batch mode.
const handleBatchMoveToFolder = () => {
  const ids = [...selectedIds.value];
  if (ids.length === 0) return;
  // Separate folders and knowledge entries - search displayCardList (includes both folders and knowledge).
  const foldersToMove: KnowledgeFolder[] = [];
  const knowledgeIds: string[] = [];
  for (const id of ids) {
    const item = displayCardList.value.find((c: KnowledgeCard) => c.id === id);
    if (item) {
      if ((item as any).isFolder) {
        foldersToMove.push(item as unknown as KnowledgeFolder);
      } else {
        knowledgeIds.push(id);
      }
    }
  }
  console.log('[BatchMove] selected:', ids.length, 'folders:', foldersToMove.length, 'knowledge:', knowledgeIds.length, 'cardList:', cardList.value.length);
  // Unified: open folder selector, move both folders and knowledge entries.
  batchMoveFolders.value = foldersToMove;
  batchMoveKnowledgeIds.value = knowledgeIds;
  loadFolderTree();
  folderMoveDialogVisible.value = true;
};

// Move sub-flow handlers for the inline card/list menu. The actual KB move is
// handled by MoveKnowledgeDialog via handleMoveKnowledge, so these just reset
// the inline menu state back to normal.
const handleMoveSelectTarget = (_kb: any) => {
  moveMenuMode.value = 'normal';
};
const handleMoveBack = () => {
  moveMenuMode.value = 'normal';
};
const handleMoveConfirm = () => {
  moveSubmitting.value = false;
  moveMenuMode.value = 'normal';
};

// Refresh knowledge files and folder tree
const handleRefresh = async () => {
  resetPage();
  await loadKnowledgeFiles(kbId.value);
  loadFolders(currentFolderId.value);
  loadFolderTree();
};

// Confirm move-to-folder: batch-move knowledge entries.
const handleMoveFolderConfirm = async (targetFolderId: string | null) => {
  if (moveFolderKnowledgeIds.value.length === 0) {
    return;
  }
  const ids = moveFolderKnowledgeIds.value;
  moveFolderKnowledgeIds.value = [];
  try {
    await batchMoveKnowledgeToFolder({ knowledge_ids: ids, folder_id: targetFolderId });
    MessagePlugin.success(t('knowledgeBase.moveToFolderSuccess'));
    handleRefresh();
  } catch (err) {
    const message = (err as { message?: string })?.message || t('knowledgeBase.moveToFolderFailed');
    MessagePlugin.error(message);
  }
};

// Open the move-folder dialog for a single folder (grid / list views).
const handleMoveFolder = (folder: KnowledgeFolder) => {
  folderToMove.value = folder;
  loadFolderTree();
  folderMoveDialogVisible.value = true;
};

// Confirm move-folder: move the folder itself or batch-move multiple folders.
const handleConfirmFolderMove = async (targetFolderId: string | null) => {
  console.log('[ConfirmFolderMove] targetFolderId:', targetFolderId, 'folderToMove:', folderToMove.value?.id, 'batchMoveFolders:', batchMoveFolders.value.length, 'batchMoveKnowledgeIds:', batchMoveKnowledgeIds.value.length);
  if (folderToMove.value) {
    // Single folder move.
    const folder = folderToMove.value;
    folderToMove.value = null;
    try {
      await moveFolder(kbId.value, folder.id, { target_parent_folder_id: targetFolderId });
      MessagePlugin.success(t('knowledgeBase.moveToFolderSuccess'));
      handleRefresh();
    } catch (err) {
      const message = (err as { message?: string })?.message || t('knowledgeBase.moveToFolderFailed');
      MessagePlugin.error(message);
    }
    return;
  }
  // Multiple folders batch move.
  if (batchMoveFolders.value.length > 0) {
    const folders = batchMoveFolders.value;
    batchMoveFolders.value = [];
    try {
      for (const folder of folders) {
        await moveFolder(kbId.value, folder.id, { target_parent_folder_id: targetFolderId });
      }
      // Also move knowledge entries if any.
      if (batchMoveKnowledgeIds.value.length > 0) {
        await batchMoveKnowledgeToFolder({ knowledge_ids: batchMoveKnowledgeIds.value, folder_id: targetFolderId });
        batchMoveKnowledgeIds.value = [];
      }
      MessagePlugin.success(t('knowledgeBase.moveToFolderSuccess'));
      handleRefresh();
    } catch (err) {
      const message = (err as { message?: string })?.message || t('knowledgeBase.moveToFolderFailed');
      MessagePlugin.error(message);
    }
  } else if (batchMoveKnowledgeIds.value.length > 0) {
    // Only knowledge entries selected, no folders.
    const ids = batchMoveKnowledgeIds.value;
    batchMoveKnowledgeIds.value = [];
    try {
      await batchMoveKnowledgeToFolder({ knowledge_ids: ids, folder_id: targetFolderId });
      MessagePlugin.success(t('knowledgeBase.moveToFolderSuccess'));
      handleRefresh();
    } catch (err) {
      const message = (err as { message?: string })?.message || t('knowledgeBase.moveToFolderFailed');
      MessagePlugin.error(message);
    }
  }
};

// Move-to-KB dialog confirmed: poll progress if async.
const handleMoveKbDialogMoved = (taskId?: string) => {
  if (taskId) {
    startMovePoll(taskId);
  } else {
    resetPage();
    loadKnowledgeFiles(kbId.value);
  }
};

const startMovePoll = (taskId: string) => {
  if (movePollTimer) clearInterval(movePollTimer);
  movePollTimer = setInterval(async () => {
    try {
      const res: any = await getKnowledgeMoveProgress(taskId);
      const data = res.data;
      if (!data) return;
      if (data.status === 'completed') {
        stopMovePoll();
        const failed = data.failed || 0;
        if (failed > 0) {
          MessagePlugin.warning(t('knowledgeBase.moveCompletedWithErrors', { success: (data.processed || 0) - failed, failed }));
        } else {
          MessagePlugin.success(t('knowledgeBase.moveCompleted'));
        }
        resetPage(); // Reset page counter when reloading files after move completion
        loadKnowledgeFiles(kbId.value);
      } else if (data.status === 'failed') {
        stopMovePoll();
        MessagePlugin.error(t('knowledgeBase.moveFailed'));
      }
    } catch {
      // ignore poll errors
    }
  }, 2000);
};

const stopMovePoll = () => {
  if (movePollTimer) {
    clearInterval(movePollTimer);
    movePollTimer = null;
  }
};

const manualEditorSuccess = ({ kbId: savedKbId }: { kbId: string; knowledgeId: string; status: 'draft' | 'publish' }) => {
  if (savedKbId === kbId.value && !isFAQ.value) {
    resetPage(); // Reset page counter when reloading files after manual edit
    loadKnowledgeFiles(savedKbId);
  }
};

const documentTitle = computed(() => {
  if (kbInfo.value?.name) {
    return `${kbInfo.value.name} · ${t('knowledgeEditor.document.title')}`;
  }
  return t('knowledgeEditor.document.title');
});

const ensureDocumentKbReady = () => {
  if (isFAQ.value) {
    MessagePlugin.warning(t('knowledgeBase.operationNotSupportedForType'));
    return false;
  }
  if (!kbId.value) {
    MessagePlugin.warning(t('knowledgeEditor.messages.missingId'));
    return false;
  }
  if (!kbInfo.value || !kbInfo.value.summary_model_id) {
    MessagePlugin.warning(t('knowledgeBase.notInitialized'));
    return false;
  }
  // Embedding model only required when RAG indexing is enabled
  const strategy = (kbInfo.value as any).indexing_strategy
  const needsEmbedding = !strategy || strategy.vector_enabled || strategy.keyword_enabled
  if (needsEmbedding && !kbInfo.value.embedding_model_id) {
    MessagePlugin.warning(t('knowledgeBase.notInitialized'));
    return false;
  }
  if (missingStorageEngine.value) {
    MessagePlugin.warning(t('knowledgeBase.missingStorageEngineUpload'));
    return false;
  }
  return true;
};


const IMAGE_EXTENSIONS = ['jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp'];
const AUDIO_EXTENSIONS = ['mp3', 'wav', 'm4a', 'flac', 'ogg'];

const uploadConfirmStore = useUploadConfirmStore();

const showUploadResultMessages = (
  successCount: number,
  failCount: number,
  totalCount: number,
  mode: 'document' | 'folder',
) => {
  if (mode === 'folder') {
    if (failCount === 0) {
      MessagePlugin.success(t('knowledgeBase.uploadAllSuccess', { count: successCount }));
    } else if (successCount > 0) {
      MessagePlugin.warning(t('knowledgeBase.uploadPartialSuccess', { success: successCount, fail: failCount }));
    } else {
      MessagePlugin.error(t('knowledgeBase.uploadAllFailed'));
    }
    return;
  }

  if (totalCount === 1) {
    if (successCount === 1) {
      MessagePlugin.success(t('knowledgeBase.uploadSuccess'));
    }
    return;
  }

  if (failCount === 0) {
    MessagePlugin.success(t('knowledgeBase.allUploadSuccess', { count: successCount }));
  } else if (successCount > 0) {
    MessagePlugin.warning(t('knowledgeBase.partialUploadSuccess', { success: successCount, fail: failCount }));
  } else {
    MessagePlugin.error(t('knowledgeBase.allUploadFailed', { count: failCount }));
  }
};

/**
 * Build KB folder structure from uploaded folder paths.
 * Maps each webkitRelativePath prefix to its KB folder_id.
 */
const buildFolderStructure = async (
  files: File[],
  targetKbId: string,
  baseParentId: string | null,
): Promise<Map<string, string>> => {
  const pathToId = new Map<string, string>();
  // Collect unique folder paths (all intermediate directory prefixes)
  const folderPaths = new Set<string>();
  for (const file of files) {
    const rp = (file as any).webkitRelativePath as string | undefined;
    if (!rp) continue;
    const parts = rp.split('/');
    // parts[0] is the top-level folder name; collect all deeper prefixes
    for (let i = 1; i < parts.length; i++) {
      folderPaths.add(parts.slice(0, i).join('/'));
    }
  }
  if (folderPaths.size === 0) return pathToId;

  // Sort by depth so parents are created before children
  const sorted = Array.from(folderPaths).sort((a, b) => a.split('/').length - b.split('/').length);

  for (const folderPath of sorted) {
    try {
      const parts = folderPath.split('/');
      const folderName = parts[parts.length - 1];
      let parentId = baseParentId;
      if (parts.length > 1) {
        const parentPath = parts.slice(0, -1).join('/');
        parentId = pathToId.get(parentPath) || baseParentId;
      }
      const created = (await createFolder(targetKbId, {
        name: folderName,
        parent_folder_id: parentId,
      })) as { id?: string } | undefined;
      if (created?.id) {
        pathToId.set(folderPath, created.id);
      }
    } catch {
      // Folder may already exist; try listing to find it
      // If creation fails, files go to base parent
    }
  }
  return pathToId;
};

// Typed accessor for the non-standard `webkitRelativePath` property set by the
// browser when a folder is selected via <input webkitdirectory>.
const relativePathOf = (file: File): string | undefined => {
  const rp = (file as File & { webkitRelativePath?: unknown }).webkitRelativePath;
  return typeof rp === 'string' && rp.length > 0 ? rp : undefined;
};

// Upload a whole folder via the batch endpoint. The server builds the folder
// tree atomically and returns a skip/error summary. Returns the same shape as
// executeUploadBatch so callers (progress UI, refresh) are unchanged.
const executeFolderUploadBatch = async (
  files: File[],
  options: { processConfig?: KnowledgeProcessOverrides; folderId?: string | null } = {},
): Promise<{ successCount: number; failCount: number }> => {
  const targetKbId = kbId.value;
  if (!targetKbId || files.length === 0) {
    return { successCount: 0, failCount: files.length };
  }

  const paths = files.map(relativePathOf).map((p) => p ?? '');
  MessagePlugin.info(t('knowledgeBase.uploadingFolder', { total: files.length }));

  try {
    const result: FolderUploadResult = await uploadKnowledgeFolder(
      targetKbId,
      files,
      paths,
      {
        root_folder_id: options.folderId ?? undefined,
        tag_ids: selectedTagIds.value.length > 0 ? [...selectedTagIds.value] : undefined,
        process_config: options.processConfig,
      },
    );
    const successCount = result.uploaded_files ?? 0;
    const failCount = (result.errors?.length ?? 0) + (result.skipped_files ?? 0);

    if (successCount > 0) {
      window.dispatchEvent(new CustomEvent('knowledgeFileUploaded', { detail: { kbId: targetKbId } }));
    }

    // Surface the skip/error summary so the user understands what happened.
    if (result.skipped_files > 0 || (result.errors?.length ?? 0) > 0) {
      MessagePlugin.warning(
        t('knowledgeBase.folderUploadPartial', {
          uploaded: successCount,
          skipped: result.skipped_files,
          errors: result.errors?.length ?? 0,
        }),
      );
    } else {
      MessagePlugin.success(t('knowledgeBase.uploadAllSuccess', { count: successCount }));
    }
    return { successCount, failCount };
  } catch (error: unknown) {
    const message = error instanceof Error ? error.message : t('knowledgeBase.uploadFailed');
    MessagePlugin.error(message);
    return { successCount: 0, failCount: files.length };
  }
};

// Upload a single .zip archive; the server extracts and rebuilds the tree.
const executeZipUpload = async (
  zipFile: File,
  options: { processConfig?: KnowledgeProcessOverrides; folderId?: string | null } = {},
): Promise<{ successCount: number; failCount: number }> => {
  const targetKbId = kbId.value;
  if (!targetKbId) {
    return { successCount: 0, failCount: 1 };
  }
  MessagePlugin.info(t('knowledgeBase.uploadingZip', { name: zipFile.name }));
  try {
    const result: FolderUploadResult = await uploadKnowledgeZip(targetKbId, zipFile, {
      root_folder_id: options.folderId ?? undefined,
      tag_ids: selectedTagIds.value.length > 0 ? [...selectedTagIds.value] : undefined,
      process_config: options.processConfig,
    });
    const successCount = result.uploaded_files ?? 0;
    const failCount = (result.errors?.length ?? 0) + (result.skipped_files ?? 0);
    if (successCount > 0) {
      window.dispatchEvent(new CustomEvent('knowledgeFileUploaded', { detail: { kbId: targetKbId } }));
    }
    if (result.skipped_files > 0 || (result.errors?.length ?? 0) > 0) {
      MessagePlugin.warning(
        t('knowledgeBase.folderUploadPartial', {
          uploaded: successCount,
          skipped: result.skipped_files,
          errors: result.errors?.length ?? 0,
        }),
      );
    } else {
      MessagePlugin.success(t('knowledgeBase.uploadAllSuccess', { count: successCount }));
    }
    return { successCount, failCount };
  } catch (error: unknown) {
    const message = error instanceof Error ? error.message : t('knowledgeBase.uploadFailed');
    MessagePlugin.error(message);
    return { successCount: 0, failCount: 1 };
  }
};

const executeUploadBatch = async (
  files: File[],
  options: { processConfig?: KnowledgeProcessOverrides; tagIds?: string[]; folderId?: string | null } = {},
) => {
  const targetKbId = kbId.value;
  if (!targetKbId || files.length === 0) {
    return { successCount: 0, failCount: files.length };
  }

  const tagIdsToUpload = options.tagIds?.length
    ? options.tagIds
    : (selectedTagIds.value.length > 0 ? [...selectedTagIds.value] : undefined);
  let successCount = 0;
  let failCount = 0;
  const totalCount = files.length;
  const hasFolderPaths = files.some((file) => {
    const relativePath = relativePathOf(file);
    return !!relativePath && relativePath.split('/').length > 1;
  });

  // Folder uploads go through the atomic batch endpoint (server builds the
  // tree and dedupes in one transaction); flat-file uploads keep the
  // per-file path below.
  if (hasFolderPaths) {
    return executeFolderUploadBatch(files, options);
  }

  for (const file of files) {
    try {
      const uploadData: {
        file: File
        tag_ids?: string[]
        fileName?: string
        folder_id?: string
        process_config?: KnowledgeProcessOverrides
      } = { file, tag_ids: tagIdsToUpload };

      // Determine which KB folder this file belongs to
      const relativePath = relativePathOf(file);
      if (relativePath) {
        const parts = relativePath.split('/');
        if (parts.length > 1) {
          // File is inside a subfolder — folder creation is handled by the
          // batch endpoint above; this branch only runs for flat uploads.
          uploadData.fileName = parts[parts.length - 1];
        }
      } else if (options.folderId) {
        uploadData.folder_id = options.folderId;
      }

      if (options.processConfig) {
        uploadData.process_config = options.processConfig;
      }

      const responseData = (await uploadKnowledgeFile(targetKbId, uploadData)) as unknown as {
        success?: boolean;
        code?: number | string;
        status?: string;
        message?: string;
        error?: { message?: string; code?: string };
      };
      const isSuccess = responseData?.success || responseData?.code === 200 || responseData?.status === 'success' || (!responseData?.error && responseData);
      if (isSuccess) {
        successCount++;
      } else {
        failCount++;
        if (totalCount === 1) {
          let errorMessage = t('knowledgeBase.uploadFailed');
          if (responseData?.error?.message) {
            errorMessage = responseData.error.message;
          } else if (responseData?.message) {
            errorMessage = responseData.message;
          }
          if (responseData?.code === 'duplicate_file' || responseData?.error?.code === 'duplicate_file') {
            errorMessage = t('knowledgeBase.fileExists');
          }
          MessagePlugin.error(errorMessage);
        }
      }
    } catch (error: unknown) {
      failCount++;
      if (totalCount === 1) {
        const err = error as { error?: { message?: string }; message?: string; code?: string };
        let errorMessage = err?.error?.message || err?.message || t('knowledgeBase.uploadFailed');
        if (err?.code === 'duplicate_file') {
          errorMessage = t('knowledgeBase.fileExists');
        }
        MessagePlugin.error(errorMessage);
      }
    }
  }

  if (successCount > 0) {
    window.dispatchEvent(new CustomEvent('knowledgeFileUploaded', {
      detail: { kbId: targetKbId },
    }));
  }

  // Flat-file path only — folder uploads short-circuit earlier.
  showUploadResultMessages(successCount, failCount, totalCount, 'document');
  return { successCount, failCount };
};

const executeUrlImport = async (url: string, processConfig?: KnowledgeProcessOverrides, tagIds?: string[], folderId?: string | null) => {
  const targetKbId = kbId.value;
  if (!targetKbId) {
    MessagePlugin.error(t('error.missingKbId'));
    return;
  }

  const tagIdsToUpload = tagIds?.length
    ? tagIds
    : (selectedTagIds.value.length > 0 ? [...selectedTagIds.value] : undefined);
  try {
    const urlData: { url: string; enable_multimodel?: boolean; tag_ids?: string[]; folder_id?: string; process_config?: KnowledgeProcessOverrides } = {
      url,
      tag_ids: tagIdsToUpload,
      process_config: processConfig,
    };
    if (folderId) {
      urlData.folder_id = folderId;
    }
    const responseData: any = await createKnowledgeFromURL(targetKbId, urlData);
    window.dispatchEvent(new CustomEvent('knowledgeFileUploaded', {
      detail: { kbId: targetKbId },
    }));
    const isSuccess = responseData?.success || responseData?.code === 200 || responseData?.status === 'success' || (!responseData?.error && responseData);
    if (isSuccess) {
      MessagePlugin.success(t('knowledgeBase.urlImportSuccess'));
    } else {
      let errorMessage = t('knowledgeBase.urlImportFailed');
      if (responseData?.error?.message) {
        errorMessage = responseData.error.message;
      } else if (responseData?.message) {
        errorMessage = responseData.message;
      }
      if (responseData?.code === 'duplicate_url' || responseData?.error?.code === 'duplicate_url') {
        errorMessage = t('knowledgeBase.urlExists');
      }
      MessagePlugin.error(errorMessage);
    }
  } catch (error: unknown) {
    const err = error as { error?: { message?: string }; message?: string; code?: string };
    let errorMessage = err?.error?.message || err?.message || t('knowledgeBase.urlImportFailed');
    if (err?.code === 'duplicate_url') {
      errorMessage = t('knowledgeBase.urlExists');
    }
    MessagePlugin.error(errorMessage);
  }
};

const handleUploadConfirmResult = async (result: UploadConfirmResult) => {
  if (result.mode === 'manual') {
    return;
  }

  const files = result.files || [];
  const urls = result.urls || [];
  const processConfig = result.processConfig;
  const tagIds = result.tagIds;
  const folderId = result.folderId;

  if (files.length > 0) {
    // A single .zip is extracted server-side into a folder tree.
    if (files.length === 1 && files[0].name.toLowerCase().endsWith('.zip')) {
      await executeZipUpload(files[0], { processConfig, folderId });
    } else {
      await executeUploadBatch(files, { processConfig, tagIds, folderId });
    }
  }

  for (const url of urls) {
    await executeUrlImport(url, processConfig, tagIds, folderId);
  }

  // Refresh current directory view
  resetPage();
  await loadKnowledgeFiles(kbId.value);
  loadTags(kbId.value, true);
  // Refresh folder list in case new folders were created
  loadFolders(currentFolderId.value);
  loadFolderTree();
};

const openUploadConfirmDialog = async (files: File[], urls: string[] = []) => {
  if (!kbInfo.value) return;
  if (files.length === 0 && urls.length === 0) return;
  try {
    const result = await uploadConfirmStore.open({
      mode: 'file',
      kbInfo: kbInfo.value,
      files,
      urls,
      acceptFileTypes: acceptFileTypes.value,
      supportedFileTypes: [...supportedFileTypes.value],
      currentFolderId: currentFolderId.value,
    });
    await handleUploadConfirmResult(result);
  } catch {
    // cancelled
  }
};

const handleUploadSourceFiles = (files: File[]) => {
  if (!ensureDocumentKbReady()) return;
  if (files.length === 0) return;
  openUploadConfirmDialog(files);
};

const handleUploadSourceUrl = (url: string) => {
  if (!ensureDocumentKbReady()) return;
  openUploadConfirmDialog([], [url]);
};

const handleManualCreate = () => {
  if (!ensureDocumentKbReady()) return;
  uiStore.openManualEditor({
    mode: 'create',
    kbId: kbId.value,
    status: 'draft',
    folderId: currentFolderId.value,
    onSuccess: manualEditorSuccess,
  });
};

const handleOpenKBSettings = () => {
  if (!kbId.value) {
    MessagePlugin.warning(t('knowledgeEditor.messages.missingId'));
    return;
  }
  uiStore.openKBSettings(kbId.value);
};

const handleNavigateToKbList = () => {
  router.push('/platform/knowledge-bases');
};

const handleNavigateToCurrentKB = () => {
  if (!kbId.value) return;
  router.push(`/platform/knowledge-bases/${kbId.value}`);
};

const handleKnowledgeDropdownSelect = (data: { value: string }) => {
  if (!data?.value) return;
  if (data.value === kbId.value) return;
  router.push(`/platform/knowledge-bases/${data.value}`);
};

const handleManualEdit = (index: number, item: KnowledgeCard) => {
  if (isFAQ.value) return;
  if (cardList.value[index]) {
    cardList.value[index].isMore = false;
  }
  uiStore.openManualEditor({
    mode: 'edit',
    kbId: item.knowledge_base_id || kbId.value,
    knowledgeId: item.id,
    onSuccess: manualEditorSuccess,
  });
};

// Opens ONLY the trace drawer for this card — does NOT pop the
// document detail drawer behind it. The trace drawer attaches to
// body so it renders independent of its host's visibility; we just
// need `details` populated so the timeline component knows which
// knowledge_id to fetch. getCardDetails resets details synchronously
// then fills asynchronously, so we re-stamp the id/parse_status
// right after the call to avoid the brief empty-id window that
// would otherwise prevent the drawer from mounting.
const docContentRef = ref<any>(null);
const handleViewTrace = (index: number, item: KnowledgeCard) => {
  if (cardList.value[index]) {
    cardList.value[index].isMore = false;
  }
  moreIndex.value = -1;
  getCardDetails(item);
  details.id = item.id;
  details.parse_status = item.parse_status;
  nextTick(() => {
    docContentRef.value?.openTimeline?.();
  });
};

const confirmRebuildKnowledge = async (index: number, item: KnowledgeCard) => {
  if (isFAQ.value) return;
  if (!canEdit.value) return;
  if (!item?.id) {
    MessagePlugin.warning(t('knowledgeEditor.messages.missingId'));
    return;
  }
  if (isParseInFlight(item.parse_status)) {
    MessagePlugin.info(t('knowledgeBase.rebuildInProgress'));
    return;
  }
  closeCardMoreMenu(index);

  // No KB context to seed the dialog defaults — fall back to a direct reparse
  // that reuses the overrides stored at upload time.
  if (!kbInfo.value) {
    await submitReparse(item.id);
    return;
  }

  // Prefill the confirm dialog with the overrides this doc was last parsed with.
  let processOverrides: KnowledgeProcessOverrides | null = item.metadata?.process_overrides ?? null;
  let fileName = item.file_name || item.title || '';
  let fileType = item.file_type || '';
  try {
    const detail: any = await getKnowledgeDetails(item.id);
    if (detail?.success && detail.data) {
      processOverrides = detail.data.metadata?.process_overrides ?? processOverrides;
      fileName = detail.data.file_name || detail.data.title || fileName;
      fileType = detail.data.file_type || fileType;
    }
  } catch {
    // fall back to the list item's fields
  }

  try {
    const result = await uploadConfirmStore.open({
      mode: 'reparse',
      kbInfo: kbInfo.value,
      reparse: { knowledgeId: item.id, fileName, fileType, processOverrides },
    });
    if (result.mode === 'reparse' && result.reparse) {
      await submitReparse(result.reparse.knowledgeId, result.processConfig);
    }
  } catch {
    // cancelled
  }
};

const submitReparse = async (id: string, processConfig?: KnowledgeProcessOverrides) => {
  try {
    await reparseKnowledge(id, processConfig ? { process_config: processConfig } : undefined);
    delete traceAvailableById[id];
    traceAvailableById[id] = true;
    MessagePlugin.success(t('knowledgeBase.rebuildSubmitted'));
    resetPage();
    loadKnowledgeFiles(kbId.value);
    scheduleWikiStatusProbes();
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.rebuildFailed'));
  }
};

const handleScroll = () => {
  if (isFAQ.value) return;
  if (docListLoading.value) return;
  if (scrollLoading) return;
  const currentKbId = kbId.value;
  if (!currentKbId) return;
  const element = knowledgeScroll.value;
  if (element) {
    let pageNum = Math.ceil(total.value / pageSize)
    const { scrollTop, scrollHeight, clientHeight } = element;
    if (scrollTop + clientHeight >= scrollHeight - 10) {
      if (cardList.value.length < total.value && page < pageNum) {
        page++;
        scrollLoading = true;
        getKnowled({ page, page_size: pageSize, ...filterParams.value }, currentKbId).finally(() => {
          if (isCurrentKb(currentKbId)) {
            scrollLoading = false;
          }
        });
      }
    }
  }
};
const getDoc = (page: number) => {
  getfDetails(details.id, page)
};

const toggleSelectRow = (id: string, checked: boolean, shiftKey?: boolean) => {
  // Use displayCardList which includes both folderCardItems and cardList
  const items = displayCardList.value;
  const idx = items.findIndex((i: KnowledgeCard) => i.id === id);
  if (shiftKey && lastSelectedIndex >= 0 && idx >= 0) {
    const [s, e] = idx < lastSelectedIndex
      ? [idx, lastSelectedIndex]
      : [lastSelectedIndex, idx];
    for (let i = s; i <= e; i++) {
      if (checked) selectedIds.value.add(items[i].id);
      else selectedIds.value.delete(items[i].id);
    }
  } else {
    if (checked) selectedIds.value.add(id);
    else selectedIds.value.delete(id);
  }
  lastSelectedIndex = idx;
  if (!checked && selectedIds.value.size === 0) {
    batchMode.value = false;
  }
};

const onCardGridCheckboxChange = (id: string, checked: boolean, ctx?: { e?: Event }) => {
  const me = ctx?.e as MouseEvent | undefined;
  toggleSelectRow(id, checked, !!me?.shiftKey);
};

const toggleSelectAll = (checked: boolean) => {
  // Only select documents, not folders
  for (const item of cardList.value) {
    if (checked) {
      selectedIds.value.add(item.id);
    } else {
      selectedIds.value.delete(item.id);
    }
  }
  if (!checked) {
    batchMode.value = false;
  }
};

const clearSelection = () => {
  selectedIds.value.clear();
  lastSelectedIndex = -1;
};

// Batch (multi-select) mode mirrors the session list's "批量管理" UX: while off,
// no checkbox is rendered so the title doesn't jitter on hover; while on,
// checkboxes are persistent and clicking a card toggles its selection.
const batchMode = ref(false);
const toggleBatchMode = () => {
  batchMode.value = !batchMode.value;
  if (!batchMode.value) clearSelection();
};
// "取消选择" / 退出批量管理：清空选择，并退出 grid 视图下的批量模式。
const handleBatchCancel = () => {
  clearSelection();
  batchMode.value = false;
};
// 切到卡片视图时，如果列表视图里已经勾选过文档，需要自动开启批量管理模式，
// 否则卡片视图默认不渲染 checkbox，会看不到勾选态。
watch(viewMode, (mode) => {
  if (mode === 'grid' && selectedIds.value.size > 0) {
    batchMode.value = true;
  }
});
// Triggered from a card / row "..." menu — match the session-list UX where
// the menu item simply opens batch mode (no auto-selection).
const handleEnterBatchFromCard = (item: any) => {
  if (item) item.isMore = false;
  moreIndex.value = -1;
  clearSelection();
  batchMode.value = true;
};
const {
  onContainerMouseDown: onDocMarqueeMouseDown,
  marqueeVisible: docMarqueeVisible,
  marqueeMode: docMarqueeMode,
  boxStyle: docMarqueeBoxStyle,
  shouldSuppressClick: shouldSuppressDocClick,
} = useMarqueeSelect({
  containerRef: knowledgeScroll,
  itemSelector: '.knowledge-card[data-select-id], .doc-list-row[data-select-id]',
  selectedIds,
  getItemId: (el) => el.dataset.selectId || null,
  enabled: computed(() => canEdit.value && !isFAQ.value && cardList.value.length > 0),
  onSelectionStart: () => {
    batchMode.value = true;
  },
});

const isManualDraftKnowledge = (item: KnowledgeCard) =>
  item.type === 'manual' && item.parse_status === 'draft';

const openKnowledgeItem = (item: KnowledgeCard) => {
  if (shouldSuppressDocClick()) return;
  if (canEdit.value && isManualDraftKnowledge(item)) {
    const index = cardList.value.findIndex((c) => c.id === item.id);
    if (index >= 0) {
      handleManualEdit(index, item);
      return;
    }
  }
  openCardDetails(item);
};

const onCardClick = (item: KnowledgeCard & { isFolder?: boolean }) => {
  if (batchMode.value) {
    onCardGridCheckboxChange(item.id, !selectedIds.value.has(item.id));
    return;
  }
  if (item.isFolder) {
    handleFolderNavigate(item.id);
    return;
  }
  openKnowledgeItem(item);
};

const confirmBatchDelete = async () => {
  if (batchDeleting.value || batchReparsing.value || selectedIds.value.size === 0) return;
  const ids = Array.from(selectedIds.value);
  const deletedIdSet = new Set(ids);
  batchDeleting.value = true;
  try {
    const res: any = await batchDeleteKnowledge(kbId.value, ids);
    if (res?.success) {
      MessagePlugin.success(t('knowledgeBase.batchDeleteSuccess', { count: ids.length }));
      // If the current folder was deleted, navigate to its parent (or root)
      if (currentFolderId.value && deletedIdSet.has(currentFolderId.value)) {
        const bc = breadcrumbPath.value || [];
        const parentFolderId = bc.length >= 2 ? bc[bc.length - 2]?.id ?? null : null;
        await navigateToFolder(parentFolderId);
      }
      clearSelection();
      batchMode.value = false;
      resetPage();
      // Reload folders immediately (folders are deleted synchronously on the backend)
      loadFolders(currentFolderId.value);
      loadFolderTree();
      // Backend enqueues knowledge deletion as async task; short-poll until
      // the list no longer contains the deleted IDs or timeout.
      const maxPolls = 30;
      const delayMs = 400;
      for (let i = 0; i < maxPolls; i++) {
        await loadKnowledgeFiles(kbId.value);
        const stillPresent = (cardList.value || []).some((c: KnowledgeCard) => deletedIdSet.has(c.id));
        if (!stillPresent) break;
        await new Promise<void>((r) => setTimeout(r, delayMs));
      }
      loadTags(kbId.value, true);
    } else {
      MessagePlugin.error(res?.message || t('knowledgeBase.batchDeleteFailed'));
    }
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('knowledgeBase.batchDeleteFailed'));
  } finally {
    batchDeleting.value = false;
  }
};

const confirmCancelParseKnowledge = async (item: KnowledgeCard) => {
  if (!item?.id) return;
  try {
    await cancelKnowledgeParse(item.id);
    MessagePlugin.success(t('knowledgeBase.cancelParseSubmitted'));
    loadKnowledgeFiles(kbId.value);
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.cancelParseFailed'));
  }
};

// Bridge card-view actions back to existing per-card handlers.
const handleCardAction = (
  action: 'edit' | 'reparse' | 'cancel-parse' | 'move' | 'move-folder' | 'delete' | 'view-trace' | 'batch-manage',
  item: KnowledgeCard,
) => {
  const idx = (cardList.value || []).findIndex((i: KnowledgeCard) => i.id === item.id);
  if (action === 'edit') return handleManualEdit(idx, item);
  if (action === 'reparse') {
    if (isParseInFlight(item.parse_status)) return onReparseMenuClick(idx, item);
    return confirmRebuildKnowledge(idx, item);
  }
  if (action === 'cancel-parse') return confirmCancelParseKnowledge(item);
  if (action === 'move') return handleMoveKnowledge(item);
  if (action === 'move-folder') return handleMoveToFolder(item);
  if (action === 'delete') return confirmDeleteKnowledge(idx, item);
  if (action === 'view-trace') return handleViewTrace(idx, item);
  if (action === 'batch-manage') return handleEnterBatchFromCard(item);
};

// Bridge list-view actions back to existing per-card handlers.
const handleListAction = (
  action: 'edit' | 'reparse' | 'cancel-parse' | 'move' | 'move-folder' | 'rename-folder' | 'tag-folder' | 'delete' | 'view-trace' | 'batch-manage',
  item: KnowledgeCard & { isFolder?: boolean },
) => {
  // Handle folder actions
  if ((item as any).isFolder) {
    if (action === 'delete') {
      confirmDeleteFolder(item as any);
    }
    if (action === 'move-folder') {
      handleMoveFolder(item as any);
    }
    if (action === 'rename-folder') {
      handleRenameFolder(item as any);
    }
    if (action === 'tag-folder') {
      openTagByFolderDialog(item as any);
    }
    return;
  }
  const idx = (cardList.value || []).findIndex((i: KnowledgeCard) => i.id === item.id);
  if (action === 'edit') return handleManualEdit(idx, item);
  if (action === 'reparse') return confirmRebuildKnowledge(idx, item);
  if (action === 'cancel-parse') return confirmCancelParseKnowledge(item);
  if (action === 'move') return handleMoveKnowledge(item);
  if (action === 'move-folder') return handleMoveToFolder(item);
  if (action === 'delete') return confirmDeleteKnowledge(idx, item);
  if (action === 'view-trace') return handleViewTrace(idx, item);
  if (action === 'batch-manage') return handleEnterBatchFromCard(item);
};

// Clear selection on filter/tag/kb change to avoid acting on hidden items.
watch(
  [selectedTagIds, docSearchKeyword, selectedFileType, selectedParseStatus, selectedSource, updatedTimeRange, kbId],
  () => {
    clearSelection();
  },
);

// After cardList reloads: stable keys rely on correct indices for shift-range; clamp anchor index.
watch(cardList, () => {
  const items = cardList.value || [];
  const n = items.length;
  if (lastSelectedIndex >= n) {
    lastSelectedIndex = n > 0 ? n - 1 : -1;
  }
  if (moreIndex.value >= n) {
    moreIndex.value = -1;
  }
  if (selectedIds.value.size === 0) return;
  const visible = new Set(items.map((i: KnowledgeCard) => i.id));
  for (const id of selectedIds.value) {
    if (!visible.has(id)) selectedIds.value.delete(id);
  }
}, { deep: false });

// 处理知识库编辑成功后的回调
const handleKBEditorSuccess = (kbIdValue: string) => {
  chatResources.invalidateKnowledgeBaseDetail(kbIdValue);
  chatResources.invalidate('knowledgeBases');
  loadKnowledgeList();
  if (kbIdValue === kbId.value) {
    loadKnowledgeBaseInfo(kbIdValue, true);
  }
};

const getTitle = (session_id: string, value: string) => {
  const now = new Date().toISOString();
  let obj = {
    title: t('knowledgeBase.newSession'),
    path: `chat/${session_id}`,
    id: session_id,
    isMore: false,
    isNoTitle: true,
    created_at: now,
    updated_at: now
  };
  usemenuStore.updataMenuChildren(obj);
  usemenuStore.changeIsFirstSession(true);
  usemenuStore.changeFirstQuery(value);
  router.push(`/platform/chat/${session_id}`);
};

async function createNewSession(value: string): Promise<void> {
  // Session 不再和知识库绑定，直接创建 Session
  createSessions({}).then(res => {
    if (res.data && res.data.id) {
      getTitle(res.data.id, value);
    } else {
      // 错误处理
      console.error(t('knowledgeBase.createSessionFailed'));
    }
  }).catch(error => {
    console.error(t('knowledgeBase.createSessionError'), error);
  });
}
</script>

<template>
  <template v-if="!isFAQ">
    <div class="knowledge-layout">
      <div class="document-header">
        <div class="document-header-title">
          <div class="document-title-row">
            <h2 class="document-breadcrumb">
              <button type="button" class="breadcrumb-link" @click="handleNavigateToKbList">
                {{ $t('menu.knowledgeBase') }}
              </button>
              <t-icon name="chevron-right" class="breadcrumb-separator" />
              <KBSwitcherDropdown v-if="knowledgeList.length" :kb-list="knowledgeList" :current-kb-id="kbId"
                @select="(id) => handleKnowledgeDropdownSelect({ value: id })">
                <button type="button" class="breadcrumb-link dropdown" :disabled="!kbId">
                  <template v-if="!kbInfo">
                    <t-skeleton animation="gradient" :row-col="[{ width: '120px', height: '20px' }]" />
                  </template>
                  <template v-else>
                    <span>{{ kbInfo.name }}</span>
                    <t-icon name="chevron-down" />
                  </template>
                </button>
              </KBSwitcherDropdown>
              <button v-else type="button" class="breadcrumb-link" :disabled="!kbId" @click="handleNavigateToCurrentKB">
                <template v-if="!kbInfo">
                  <t-skeleton animation="gradient" :row-col="[{ width: '120px', height: '20px' }]" />
                </template>
                <template v-else>
                  {{ kbInfo.name }}
                </template>
              </button>
              <t-icon name="chevron-right" class="breadcrumb-separator" />
              <template v-if="isWiki">
                <span :class="['breadcrumb-tab', { active: activeKbTab === 'documents' }]"
                  @click="activeKbTab = 'documents'">{{ $t('knowledgeEditor.wikiBrowser.tabDocuments') }}</span>
                <span class="breadcrumb-tab-sep">/</span>
                <span :class="['breadcrumb-tab', { active: activeKbTab === 'wiki', indexing: wikiIsIndexing }]"
                  @click="activeKbTab = 'wiki'">
                  Wiki
                  <t-tooltip v-if="wikiIsIndexing" :content="wikiIndexingTip" placement="bottom">
                    <t-loading size="small" class="breadcrumb-tab-indicator" />
                  </t-tooltip>
                </span>
                <span class="breadcrumb-tab-sep">/</span>
                <t-tooltip :content="$t('knowledgeEditor.wikiBrowser.tabGraphTip')" placement="bottom">
                  <span :class="['breadcrumb-tab', { active: activeKbTab === 'graph', indexing: wikiIsIndexing }]"
                    @click="activeKbTab = 'graph'">
                    {{ $t('knowledgeEditor.wikiBrowser.tabGraph') }}
                    <t-tooltip v-if="wikiIsIndexing" :content="wikiIndexingTip" placement="bottom">
                      <t-loading size="small" class="breadcrumb-tab-indicator" />
                    </t-tooltip>
                  </span>
                </t-tooltip>
              </template>
              <span v-else class="breadcrumb-current">{{ $t('knowledgeEditor.document.title') }}</span>
            </h2>
            <!-- 标题行右侧的动作锚点：聚拢"信息"和"设置"两个圆形按钮。 -->
            <div class="kb-title-actions">
              <KBInfoPopover v-if="kbInfo && !authStore.isLiteMode" :kb-info="kbInfo"
                :supported-file-types="[...supportedFileTypes]" />
              <t-tooltip v-if="canManage" :content="$t('knowledgeBase.settings')" placement="top">
                <button type="button" class="kb-settings-button" :disabled="!kbId" @click="handleOpenKBSettings">
                  <t-icon name="setting" size="16px" />
                </button>
              </t-tooltip>
            </div>
          </div>
          <p class="document-subtitle">{{ $t('knowledgeEditor.document.subtitle') }}</p>
          <p v-if="unsupportedFileTypes.length" class="parser-hint" @click="goToParserSettings">
            <t-icon name="info-circle" class="parser-hint-icon" />
            <span>{{$t('knowledgeBase.unsupportedTypesHint', {
              types: unsupportedFileTypes.map(t => '.' + t).join('、')
            })
              }}</span>
            <span class="parser-hint-link">{{ $t('knowledgeBase.goToParserSettings') }} →</span>
          </p>
          <p v-if="missingStorageEngine" class="storage-engine-warning" @click="handleOpenKBSettings">
            <t-icon name="info-circle" class="warning-icon" />
            <span>{{ $t('knowledgeBase.missingStorageEngine') }}</span>
            <span class="warning-link">{{ $t('knowledgeBase.goToStorageSettings') }} →</span>
          </p>
        </div>
      </div>

      <!-- Wiki Browser / Graph (shown when wiki or graph tab is active) -->
      <div v-if="isWiki && (activeKbTab === 'wiki' || activeKbTab === 'graph')" class="wiki-main-area">
        <WikiBrowser v-if="kbId" :knowledge-base-id="kbId" :view="activeKbTab === 'graph' ? 'graph' : 'browser'"
          :can-edit="canEdit" @open-source-doc="openSourceDoc" @status-change="onWikiStatusChange"
          @view-graph="onViewWikiInGraph" />
      </div>

      <template v-if="activeKbTab === 'documents' || !isWiki">
        <!-- Folder breadcrumb: always rendered (v-show) to avoid layout jitter -->
        <div v-show="!isFAQ" class="folder-breadcrumb-bar">
          <t-icon name="folder-open" class="folder-breadcrumb-icon" />
          <button type="button" class="folder-breadcrumb-link" @click="handleFolderNavigate(null)">
            {{ $t('knowledgeFolder.rootFolder') }}
          </button>
          <template v-for="(f, idx) in breadcrumbPath" :key="f.id">
            <t-icon name="chevron-right" class="folder-breadcrumb-sep" />
            <button
              type="button"
              class="folder-breadcrumb-link"
              :class="{ current: idx === breadcrumbPath.length - 1 }"
              @click="handleFolderNavigate(f.id)"
            >
              {{ f.name }}
            </button>
          </template>
        </div>
        <div class="knowledge-main">
          <div class="tag-content">
            <div class="doc-card-area">
              <div class="doc-filter-bar">
                <IconButtonGroup>
                  <IconButton v-if="canEdit" :icon="'folder-add'" :tooltip="$t('knowledgeFolder.createFolder')" @click="handleCreateFolder" />
                  <IconButton :icon="'refresh'" :tooltip="$t('knowledgeBase.refresh')" @click="handleRefresh" />
                </IconButtonGroup>
                <t-input v-model.trim="docSearchKeyword" :placeholder="$t('knowledgeBase.docSearchPlaceholder')"
                  clearable class="doc-search-input" @clear="loadKnowledgeFiles(kbId)"
                  @enter="loadKnowledgeFiles(kbId)">
                  <template #prefix-icon>
                    <t-icon name="search" size="16px" />
                  </template>
                </t-input>
                <t-checkbox v-if="currentFolderId" v-model="searchInFolder" class="doc-search-in-folder"
                  :title="$t('knowledgeBase.searchInFolder')"
                  @change="() => { resetPage(); loadKnowledgeFiles(kbId); }">
                  <t-icon name="folder" size="14px" style="vertical-align: -2px; margin-right: 2px;" />
                  {{ $t('knowledgeBase.searchInFolder') }}
                </t-checkbox>
                <div class="doc-filter-bar__filters">
                <t-popup v-model:visible="tagFilterPanelVisible" trigger="click" placement="bottom-left"
                  overlay-class-name="tag-filter-popup" :overlay-inner-style="{ padding: 0 }">
                  <template #content>
                    <div class="tag-filter-panel" @click.stop>
                      <div class="tag-filter-panel__header">
                        <div class="tag-filter-panel__title">
                          <span>{{ $t('knowledgeBase.tagFilterTitle') }}</span>
                          <span class="tag-filter-panel__count">({{ sidebarCategoryCount }})</span>
                        </div>
                      </div>
                      <div class="tag-search-bar">
                        <t-input v-model.trim="tagSearchQuery" size="small"
                          :placeholder="$t('knowledgeBase.tagSearchPlaceholder')" clearable>
                          <template #prefix-icon>
                            <t-icon name="search" size="14px" />
                          </template>
                        </t-input>
                      </div>
                      <div class="tag-filter-panel__body">
                        <template v-if="tagLoading && !sidebarTags.length">
                          <div class="tag-filter-chips">
                            <div v-for="n in 8" :key="'skel-tag-' + n" class="tag-filter-chip-skeleton">
                              <t-skeleton animation="gradient"
                                :row-col="[{ width: '56px', height: '24px', type: 'rect' }]" />
                            </div>
                          </div>
                        </template>
                        <template v-else>
                          <div class="tag-filter-chips">
                            <button
                              v-for="tag in sidebarTags"
                              :key="tag.id"
                              type="button"
                              class="tag-filter-chip"
                              :class="{ active: isTagFilterActive(tag.id) }"
                              :title="`${tag.name} (${tag.knowledge_count || 0})`"
                              @click="handleTagRowClick(tag.id)"
                            >
                              <span class="tag-filter-chip__label">{{ tag.name }}</span>
                              <span class="tag-filter-chip__count">{{ tag.knowledge_count || 0 }}</span>
                            </button>
                          </div>
                          <div v-if="!sidebarTags.length" class="tag-empty-state">
                            {{ $t('knowledgeBase.tagEmptyResult') }}
                          </div>
                          <div v-if="tagHasMore" class="tag-load-more">
                            <t-button variant="text" size="small" :loading="tagLoadingMore"
                              @click.stop="kbId && loadTags(kbId)">
                              {{ $t('tenant.loadMore') }}
                            </t-button>
                          </div>
                        </template>
                      </div>
                      <div v-if="canEdit" class="tag-filter-panel__footer">
                        <t-button variant="text" size="small" class="tag-manage-link" @click="openTagManageDrawer">
                          {{ $t('knowledgeBase.tagManageLink') }}
                        </t-button>
                      </div>
                    </div>
                  </template>
                  <div class="doc-filter-field">
                    <button type="button" class="doc-tag-filter-trigger doc-filter-field__control"
                      :class="{ open: tagFilterPanelVisible, 'is-placeholder': isTagFilterPlaceholder }"
                      :aria-label="$t('knowledgeBase.tagFilterTitle')"
                      :title="activeTagFilterTitle"
                      @mouseenter="tagFilterTriggerHover = true"
                      @mouseleave="tagFilterTriggerHover = false">
                      <span class="doc-tag-filter-trigger__prefix" aria-hidden="true">
                        <t-icon name="discount" size="16px" />
                      </span>
                      <span class="doc-tag-filter-trigger__label">{{ activeTagFilterLabel }}</span>
                      <span class="doc-tag-filter-trigger__suffix">
                        <span
                          v-if="showTagFilterClear"
                          class="t-input__suffix t-input__suffix-icon t-input__clear"
                          :aria-label="$t('common.clear')"
                          @click.stop="clearTagFilter"
                          @mousedown.stop
                        >
                          <t-icon name="close-circle-filled" class="t-input__suffix-clear" />
                        </span>
                        <t-icon
                          v-else
                          name="chevron-down"
                          size="16px"
                          class="doc-tag-filter-trigger__caret"
                          :class="{ open: tagFilterPanelVisible }"
                        />
                      </span>
                    </button>
                  </div>
                </t-popup>
                <div class="doc-filter-field">
                  <t-select v-model="selectedFileType" :options="fileTypeOptions"
                    :placeholder="$t('knowledgeBase.fileTypeFilter')" class="doc-type-select doc-filter-field__control"
                    clearable>
                    <template #prefixIcon>
                      <t-icon name="file" size="16px" />
                    </template>
                  </t-select>
                </div>
                <div class="doc-filter-field">
                  <t-select v-model="selectedParseStatus" :options="parseStatusOptions"
                    :placeholder="$t('knowledgeBase.parseStatusFilter')" class="doc-type-select doc-filter-field__control"
                    clearable>
                    <template #prefixIcon>
                      <t-icon name="check-circle" size="16px" />
                    </template>
                  </t-select>
                </div>
                <div class="doc-filter-field">
                  <t-select v-model="selectedSource" :options="sourceOptions"
                    :placeholder="$t('knowledgeBase.sourceFilter')" class="doc-type-select doc-filter-field__control"
                    clearable>
                    <template #prefixIcon>
                      <t-icon name="link" size="16px" />
                    </template>
                  </t-select>
                </div>
                <div class="doc-filter-field doc-filter-field--wide">
                  <t-date-range-picker v-model="updatedTimeRange"
                    :placeholder="[$t('knowledgeBase.updatedTimeFrom'), $t('knowledgeBase.updatedTimeTo')]"
                    :disable-date="disableFutureDate" class="doc-date-range doc-filter-field__control" clearable
                    allow-input>
                    <template #prefixIcon>
                      <t-icon name="time" size="16px" />
                    </template>
                  </t-date-range-picker>
                </div>
                </div>
                <div class="doc-filter-bar__trailing">
                  <div class="doc-view-toggle" role="group" :aria-label="$t('knowledgeBase.viewModeToggle')">
                    <t-tooltip :content="$t('knowledgeBase.viewModeGrid')" placement="top">
                      <button type="button" class="doc-view-toggle-btn" :class="{ active: viewMode === 'grid' }"
                        @click="viewMode = 'grid'" :aria-pressed="viewMode === 'grid'">
                        <t-icon name="view-module" size="16px" />
                      </button>
                    </t-tooltip>
                    <t-tooltip :content="$t('knowledgeBase.viewModeList')" placement="top">
                      <button type="button" class="doc-view-toggle-btn" :class="{ active: viewMode === 'list' }"
                        @click="viewMode = 'list'" :aria-pressed="viewMode === 'list'">
                        <t-icon name="view-list" size="16px" />
                      </button>
                    </t-tooltip>
                  </div>
                <IconButtonGroup v-if="canEdit" style="margin-left: auto">
                  <KbUploadSourceDropdown ref="uploadSourceRef" :accept-file-types="acceptFileTypes"
                    :supported-file-types="[...supportedFileTypes]" include-manual trigger-icon="file-add"
                    data-guide="kb-detail-add-doc"
                    :tooltip="t('knowledgeBase.addDocument')" placement="bottom-right" @files="handleUploadSourceFiles"
                    @url="handleUploadSourceUrl" @manual="handleManualCreate" />
                </IconButtonGroup>
                </div>
              </div>
              <div class="doc-scroll-container"
                :class="{ 'is-empty': !displayCardList.length && !docListLoading, 'is-marquee-active': docMarqueeVisible }"
                ref="knowledgeScroll" @scroll="handleScroll" @mousedown="onDocMarqueeMouseDown">
                <div v-if="docMarqueeVisible" class="doc-marquee-box"
                  :class="{ 'is-add': docMarqueeMode === 'add', 'is-subtract': docMarqueeMode === 'subtract' }"
                  :style="docMarqueeBoxStyle" aria-hidden="true" />
                <!-- 文档骨架屏 -->
                <div v-if="docListLoading && cardList.length === 0" class="doc-card-list doc-card-list-animated">
                  <div v-for="n in 8" :key="'doc-skel-' + n" class="knowledge-card knowledge-card-skeleton">
                    <div class="card-content">
                      <div class="card-content-nav">
                        <t-skeleton animation="gradient" :row-col="[{ width: '70%', height: '18px' }]" />
                      </div>
                      <t-skeleton animation="gradient"
                        :row-col="[{ width: '100%', height: '14px' }, { width: '60%', height: '14px' }]" />
                    </div>
                    <div class="card-bottom">
                      <t-skeleton animation="gradient"
                        :row-col="[[{ width: '80px', height: '14px' }, { width: '40px', height: '18px', type: 'rect' }]]" />
                    </div>
                  </div>
                </div>
                <template v-else-if="displayCardList.length && viewMode === 'grid'">
                  <div class="doc-card-list doc-card-list-animated">
                    <!-- 文件夹卡片 -->
                    <div v-for="f in folderCardItems" :key="f.id"
                      class="knowledge-card knowledge-card--folder"
                      @click="handleFolderNavigate(f.id)">
                      <div class="card-content">
                        <div class="card-content-nav">
                          <t-icon name="folder" size="20px" class="folder-card-icon" />
                          <span class="card-content-title" :title="f.file_name">{{ f.file_name }}</span>
                          <t-popup v-if="canEdit" trigger="click" placement="bottom-right" destroy-on-close>
                            <button type="button" class="folder-card-more" @click.stop>
                              <t-icon name="more" size="16px" />
                            </button>
                            <template #content>
                              <div class="folder-card-menu">
                                <div class="folder-card-menu-item" @click.stop="handleRenameFolder(f)">
                                  <t-icon name="edit" size="16px" />
                                  <span>{{ $t('knowledgeFolder.renameFolder') }}</span>
                                </div>
                                <div class="folder-card-menu-item" @click.stop="handleMoveFolder(f)">
                                  <t-icon name="folder-import" size="16px" />
                                  <span>{{ $t('knowledgeFolder.moveFolder') }}</span>
                                </div>
                                <div class="folder-card-menu-item" @click.stop="openTagByFolderDialog(f)">
                                  <t-icon name="discount" size="16px" />
                                  <span>{{ $t('tagByFolder.menuLabel') }}</span>
                                </div>
                                <t-popconfirm theme="warning"
                                  :content="$t('knowledgeFolder.confirmDeleteFolder', { name: f.file_name || '' })"
                                  :confirm-btn="{ content: $t('common.confirm'), theme: 'danger' }"
                                  :cancel-btn="{ content: $t('common.cancel') }" placement="left"
                                  @confirm="confirmDeleteFolder(f)">
                                  <div class="folder-card-menu-item danger">
                                    <t-icon name="delete" size="16px" />
                                    <span>{{ $t('knowledgeFolder.deleteFolder') }}</span>
                                  </div>
                                </t-popconfirm>
                              </div>
                            </template>
                          </t-popup>
                        </div>
                      </div>
                      <div class="card-bottom">
                        <span class="card-bottom-tag">{{ $t('knowledgeFolder.itemCount', { count: f.knowledge_count || 0 }) }}</span>
                        <span class="card-bottom-time">{{ new Date(f.created_at).toLocaleDateString() }}</span>
                      </div>
                    </div>
                    <!-- 文档卡片 -->
                    <DocumentCardView
                      inline
                    :items="cardList"
                    :selected-ids="selectedIds"
                    :batch-mode="batchMode"
                    :can-edit="canEdit"
                    :can-mutate-knowledge="canMutateKnowledge"
                    :trace-available-by-id="traceAvailableById"
                    :tag-list="tagList"
                    :folder-tree-present="folderTree.length > 0"
                    :move-menu-mode="moveMenuMode"
                    :move-target-kbs="moveTargetKbs"
                    :move-targets-loading="moveTargetsLoading"
                    :move-selected-target-name="moveSelectedTargetName"
                    :move-mode="moveMode"
                    :move-submitting="moveSubmitting"
                    @open="(item: any) => openKnowledgeItem(item)"
                    @toggle-checkbox="onCardGridCheckboxChange"
                    @menu-visible-change="(visible: boolean, item: any) => onCardMoreVisibleChange(visible, item)"
                    @action="(action: any, item: any) => handleCardAction(action, item)"
                    @tag-edit="(item: any) => openTagEditDialog(item)"
                    @move-select-target="(kb: any) => handleMoveSelectTarget(kb)"
                    @move-back="handleMoveBack"
                    @move-confirm="handleMoveConfirm"
                    @update:move-mode="(mode: any) => moveMode = mode"
                  />
                  </div>
                </template>
                <template v-else-if="displayCardList.length && viewMode === 'list'">
                  <DocumentListView :items="displayCardList" :selected-ids="selectedIds" :tag-list="tagList"
                    :can-edit="canEdit" :can-mutate-knowledge="canMutateKnowledge"
                    :trace-visible-ids="traceAvailableById"
                    :folder-tree-present="folderTree.length > 0"
                    :move-menu-mode="moveMenuMode"
                    :move-target-kbs="moveTargetKbs"
                    :move-targets-loading="moveTargetsLoading"
                    :move-selected-target-name="moveSelectedTargetName"
                    :move-mode="moveMode"
                    :move-submitting="moveSubmitting"
                    @open="(item: any) => openKnowledgeItem(item)"
                    @enter-folder="(folderId: string) => handleFolderNavigate(folderId)"
                    @move-folder="(item: any) => handleMoveFolder(item)"
                    @toggle-row="toggleSelectRow"
                    @toggle-all="toggleSelectAll" @action="(action: any, item: any) => handleListAction(action, item)"
                    @probe-trace="(item: any) => probeTraceAvailable(item)"
                    @tag-edit="(item: any) => openTagEditDialog(item)"
                    @move-select-target="(kb: any) => handleMoveSelectTarget(kb)"
                    @move-back="handleMoveBack"
                    @move-confirm="handleMoveConfirm"
                    @update:move-mode="(mode: any) => moveMode = mode"
                    @reset-move-state="moveMenuMode = 'normal'" />
                </template>
                <template v-else-if="!docListLoading">
                  <div class="doc-empty-state">
                    <EmptyKnowledge />
                  </div>
                </template>
              </div>
              <div class="doc-batch-bar-anchor" v-show="batchMode || selectedIds.size > 0">
                <DocumentBatchBar :count="selectedIds.size" :delete-loading="batchDeleting"
                  :reparse-loading="batchReparsing" :batch-tag-loading="batchTagLoading"
                  :visible="batchMode || selectedIds.size > 0"
                  @cancel="handleBatchCancel" @delete="confirmBatchDelete" @reparse="confirmBatchReparse"
                  @batch-tag="openBatchTagDialog" @move-folder="handleBatchMoveToFolder" />
              </div>
            </div>
          </div>
        </div>
      </template>

      <!-- DocContent drawer (shared by documents tab and wiki source refs) -->
      <DocContent ref="docContentRef" :visible="isCardDetails" :details="details" :canEditKB="canEdit"
        :canDownloadKB="canDownloadKnowledge" :kbId="kbId"
        @closeDoc="closeDoc" @getDoc="getDoc">
      </DocContent>
    </div>
  </template>
  <template v-else>
    <div class="faq-manager-wrapper">
      <FAQEntryManager v-if="kbId" :kb-id="kbId" />
    </div>
  </template>

  <!-- 知识库编辑器（创建/编辑统一组件） -->
  <KnowledgeBaseEditorModal :visible="uiStore.showKBEditorModal" :mode="uiStore.kbEditorMode"
    :kb-id="uiStore.currentKBId || undefined" :initial-type="uiStore.kbEditorType"
    @update:visible="(val) => val ? null : uiStore.closeKBEditor()" @success="handleKBEditorSuccess" />

  <ContextualGuide tour="kbDetail" :when="showKbDetailContextualGuide" />

  <!-- 标签编辑弹窗 -->
  <TagEditDialog :visible="tagEditDialogVisible"
    :knowledge-name="tagEditTarget?.display_name || tagEditTarget?.file_name || tagEditTarget?.title || ''"
    :kb-id="kbId" :tag-list="tagList" :selected-tags="tagEditTarget?.tags || []" :can-manage="canEdit"
    @update:visible="tagEditDialogVisible = $event" @confirm="onTagEditConfirm" @tag-created="loadTags(kbId, true)"
    @open-manage="openTagManageFromEditDialog" />

  <!-- 批量改标签弹窗 -->
  <TagEditDialog :visible="batchTagDialogVisible"
    :knowledge-name="$t('knowledgeBase.selectedCount', { count: selectedIds.size })"
    :kb-id="kbId" :tag-list="tagList" :selected-tags="batchTagInitialTags" :can-manage="canEdit" batch-mode
    @update:visible="batchTagDialogVisible = $event" @confirm="confirmBatchTag" @tag-created="loadTags(kbId, true)"
    @open-manage="openTagManageFromEditDialog" />

  <KbTagManageDrawer
    v-if="!isFAQ"
    v-model:visible="tagManageDrawerVisible"
    :kb-id="kbId"
    :is-faq="isFAQ"
    @changed="onTagManageChanged"
  />

  <!-- Tag-by-folder dialog -->
  <TagByFolderDialog
    v-model:visible="tagByFolderDialogVisible"
    :kb-id="kbId"
    :folder-id="tagByFolderTarget?.id || ''"
    :folder-name="tagByFolderTarget?.name || ''"
    :tag-list="tagList"
    :loading="tagByFolderLoading"
    @confirm="confirmTagByFolder"
    @tag-created="loadTags(kbId, true)"
  />

  <!-- Folder management dialog -->
  <FolderManageDialog
    v-model:visible="folderDialogVisible"
    :kb-id="kbId"
    :mode="folderDialogMode"
    :folder="currentEditFolder"
    :parent-folder-id="currentFolderId"
    @success="handleFolderDialogSuccess"
  />

  <!-- Move knowledge to another KB (shared by grid + list views) -->
  <MoveKnowledgeDialog
    v-model:visible="moveKbDialogVisible"
    :knowledge-ids="moveKbDialogIds"
    :source-kb-id="kbId"
    @moved="handleMoveKbDialogMoved"
  />

  <!-- Move knowledge to a folder within the current KB -->
  <FolderSelector
    v-model:visible="moveFolderDialogVisible"
    :title="t('knowledgeBase.moveToFolder')"
    :folder-tree="folderTree"
    :tree-loading="treeLoading"
    :current-folder-id="currentFolderId"
    @confirm="handleMoveFolderConfirm"
  />

  <!-- Move a folder itself to another parent folder within the current KB -->
  <FolderSelector
    v-model:visible="folderMoveDialogVisible"
    :title="t('knowledgeFolder.moveFolder')"
    :folder-tree="folderTree"
    :tree-loading="treeLoading"
    :current-folder-id="folderToMove?.parent_folder_id || null"
    :disabled-folder-ids="folderMoveDisabledIds"
    @confirm="handleConfirmFolderMove"
  />
</template>
<style>
/* 下拉菜单容器样式已统一至 @/assets/dropdown-menu.less */
.tag-filter-popup {
  z-index: 5500 !important;
}

.tag-filter-popup .t-popup__content {
  padding: 0 !important;
  border-radius: 8px !important;
  background: var(--td-bg-color-container) !important;
  border: 0.5px solid var(--td-component-stroke) !important;
  box-shadow:
    0 0 0 0.5px rgba(0, 0, 0, 0.03),
    0 2px 4px rgba(0, 0, 0, 0.04),
    0 8px 24px rgba(0, 0, 0, 0.1) !important;
}

.tag-more-popup .tag-menu {
  display: flex;
  flex-direction: column;
}

.tag-more-popup .tag-menu-item {
  display: flex;
  align-items: center;
  padding: 8px 16px;
  cursor: pointer;
  transition: all 0.2s ease;
  color: var(--td-text-color-primary);
  font-family: var(--app-font-family);
  font-size: 14px;
  font-weight: 400;
}

.tag-more-popup .tag-menu-item .menu-icon {
  margin-right: 8px;
  font-size: 16px;
}

.tag-more-popup .tag-menu-item:hover {
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-primary);
}
</style>
<style scoped lang="less">
.knowledge-layout {
  display: flex;
  flex-direction: column;
  margin: 0 16px 0 4px;
  gap: 20px;
  height: 100%;
  flex: 1;
  width: 100%;
  min-width: 0;
  padding: 24px 32px 0px;
  box-sizing: border-box;
}

// Breadcrumb tab switch (文档/Wiki in breadcrumb)
.breadcrumb-tab {
  cursor: pointer;
  color: var(--td-text-color-placeholder);
  font-weight: 400;
  transition: color 0.15s;
  display: inline-flex;
  align-items: center;
  gap: 4px;

  &:hover {
    color: var(--td-text-color-primary);
  }

  &.active {
    color: var(--td-brand-color);
    font-weight: 600;
  }

  &.indexing {
    color: var(--td-brand-color);
  }
}

.breadcrumb-tab-indicator {
  display: inline-flex;
  align-items: center;
  color: var(--td-brand-color);
  font-size: 12px;
  line-height: 1;
}

.breadcrumb-tab-sep {
  margin: 0 6px;
  color: var(--td-text-color-disabled);
  font-weight: 400;
}

.wiki-main-area {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

// 与列表页一致：浅灰底圆角区，左侧筛选为白底卡片
.knowledge-main {
  display: flex;
  flex: 1;
  min-height: 0;
  background: transparent;
  border: none;
}

// 标签筛选浮层：点击工具栏入口展开，不占文档列表横向空间
.tag-filter-panel {
  width: 320px;
  max-width: min(320px, calc(100vw - 32px));
  max-height: min(70vh, 480px);
  display: flex;
  flex-direction: column;
  padding: 12px 14px;
  box-sizing: border-box;
  font-size: 12px;
  color: var(--td-text-color-primary);

  .tag-filter-panel__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 10px;
    padding: 0;
    color: var(--td-text-color-primary);
  }

  .tag-filter-panel__title {
    display: flex;
    align-items: baseline;
    gap: 6px;
    font-size: 14px;
    font-weight: 600;
    letter-spacing: 0.5px;
  }

  .tag-filter-panel__count {
    font-size: 12px;
    color: var(--td-text-color-placeholder);
    font-weight: 400;
  }

  .tag-search-bar {
    margin-bottom: 10px;
    padding: 0;

    :deep(.t-input) {
      font-size: 13px;
      background-color: var(--td-bg-color-secondarycontainer);
      border-color: transparent;
      border-radius: 6px;
      box-shadow: none !important;

      &:hover,
      &:focus,
      &.t-is-focused {
        border-color: var(--td-component-border);
        background-color: var(--td-bg-color-container);
        box-shadow: none !important;
      }
    }

    :deep(.t-input__inner) {
      font-size: 13px;
    }

    :deep(.t-input__prefix-icon) {
      margin-right: 0;
    }
  }

  .tag-filter-panel__body {
    display: flex;
    flex-direction: column;
    gap: 8px;
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    scrollbar-width: thin;

    &::-webkit-scrollbar {
      width: 4px;
    }

    &::-webkit-scrollbar-thumb {
      border-radius: 2px;
      background: var(--td-scrollbar-color);
    }
  }

  .tag-filter-chips {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: 6px;
  }

  .tag-filter-chip-skeleton {
    flex-shrink: 0;
  }

  .tag-filter-chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    box-sizing: border-box;
    max-width: 100%;
    height: 24px;
    padding: 0 8px;
    border: 1px solid var(--td-component-stroke);
    border-radius: 4px;
    background: transparent;
    color: var(--td-text-color-secondary);
    font-family: var(--app-font-family);
    font-size: 11px;
    font-weight: 400;
    line-height: 24px;
    cursor: pointer;
    outline: none;
    transition: background 0.15s ease, color 0.15s ease, border-color 0.15s ease;
    -webkit-font-smoothing: antialiased;

    &:hover:not(.active) {
      border-color: var(--td-component-border);
      background: var(--td-bg-color-secondarycontainer);
      color: var(--td-text-color-primary);
    }

    &:focus-visible {
      box-shadow: 0 0 0 2px color-mix(in srgb, var(--td-component-stroke) 60%, transparent);
    }

    &.active {
      border-color: color-mix(in srgb, var(--td-brand-color) 35%, var(--td-component-stroke));
      color: var(--td-brand-color);
      font-weight: 500;
      background-color: color-mix(in srgb, var(--td-brand-color) 6%, transparent);

      .tag-filter-chip__count {
        color: color-mix(in srgb, var(--td-brand-color) 72%, var(--td-text-color-secondary));
      }

      &:hover {
        background-color: color-mix(in srgb, var(--td-brand-color) 10%, transparent);
      }
    }
  }

  .tag-filter-chip__label {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 120px;
  }

  .tag-filter-chip__count {
    flex-shrink: 0;
    font-size: 10px;
    font-weight: 400;
    font-variant-numeric: tabular-nums;
    color: var(--td-text-color-placeholder);

    &::before {
      content: '·';
      margin-right: 2px;
      opacity: 0.65;
    }
  }

  .tag-filter-panel__footer {
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--td-component-stroke);
    display: flex;
    justify-content: flex-start;

    :deep(.tag-manage-link.t-button) {
      padding: 0;
      height: auto;
      min-height: 0;
      font-size: 13px;
      color: var(--td-text-color-secondary);
      border: none !important;
      background: transparent !important;
      box-shadow: none !important;
      transition: color 0.15s ease;

      &:hover,
      &:focus-visible {
        color: var(--td-brand-color) !important;
        background: transparent !important;
        border-color: transparent !important;
        text-decoration: none;
      }
    }
  }

  .tag-load-more {
    display: flex;
    justify-content: center;
    padding-top: 2px;

    :deep(.t-button) {
      padding: 0;
      font-size: 12px;
      color: var(--td-text-color-placeholder);
    }
  }

  .tag-empty-state {
    text-align: center;
    padding: 6px 0;
    color: var(--td-text-color-placeholder);
    font-size: 12px;
  }
}

.tag-content {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  min-height: 0;
  padding: 0;
  border: none;
  overflow: hidden;
  background: transparent;
}

.doc-card-area {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  position: relative;
  /* 作为批量工具栏悬浮的定位上下文 */
}

.doc-filter-bar {
  padding: 0 0 12px 0;
  flex-shrink: 0;
  display: grid;
  grid-template-columns: 1fr auto;
  grid-template-areas:
    'search trailing'
    'filters filters';
  gap: 8px 12px;
  align-items: center;

  .doc-search-input {
    grid-area: search;
    min-width: 0;
    width: 100%;
  }

  &__filters {
    grid-area: filters;
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
    overflow-x: auto;
    flex-wrap: nowrap;
    -webkit-overflow-scrolling: touch;
    scrollbar-width: thin;
    scrollbar-color: rgba(0, 0, 0, 0.15) transparent;

    &::-webkit-scrollbar {
      height: 4px;
    }

    &::-webkit-scrollbar-thumb {
      background-color: rgba(0, 0, 0, 0.15);
      border-radius: 2px;
    }
  }

  &__trailing {
    grid-area: trailing;
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  @media (min-width: 1280px) {
    display: flex;
    flex-direction: row;
    flex-wrap: nowrap;
    gap: 12px;

    &__filters {
      flex: 0 1 auto;
      overflow-x: visible;
    }
  }

  .doc-filter-field {
    width: 140px;
    flex-shrink: 0;

    &--wide {
      width: 280px;
    }

    &__control {
      width: 100%;
    }
  }

  .doc-tag-filter-trigger {
    display: inline-flex;
    align-items: center;
    box-sizing: border-box;
    width: 100%;
    height: 32px;
    padding: 0 8px;
    border: 1px solid transparent;
    border-radius: var(--td-radius-default);
    background: var(--td-bg-color-secondarycontainer);
    color: var(--td-text-color-primary);
    font-family: var(--app-font-family);
    font-size: 14px;
    line-height: 1;
    cursor: pointer;
    transition: background 0.2s ease, border-color 0.2s ease;

    &:hover,
    &.open {
      background: var(--td-bg-color-secondarycontainer);
      border-color: transparent;
    }

    &.is-placeholder {
      color: var(--td-text-color-placeholder);
    }

    &__prefix {
      flex-shrink: 0;
      display: inline-flex;
      align-items: center;
      margin-right: var(--td-comp-margin-s);
      color: var(--td-text-color-placeholder);
    }

    &__label {
      flex: 1;
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      text-align: left;
    }

    &__suffix {
      flex-shrink: 0;
      display: inline-flex;
      align-items: center;
      margin-left: var(--td-comp-margin-s);

      :deep(.t-input__suffix) {
        margin-left: 0;
      }

      :deep(.t-input__suffix-clear) {
        font-size: 16px;
      }
    }

    &__caret {
      flex-shrink: 0;
      color: var(--td-text-color-placeholder);
      transition: transform 0.2s ease, color 0.2s ease;

      &.open {
        color: var(--td-brand-color);
        transform: rotate(180deg);
      }
    }
  }

  @media (min-width: 1280px) {
    .doc-search-input {
      flex: 1 1 220px;
      min-width: 220px;
    }
  }

  .doc-search-in-folder {
    flex-shrink: 0;
    font-size: 13px;
    color: var(--td-text-color-secondary);
    white-space: nowrap;
    user-select: none;
  }

  .doc-type-select {
    width: 100%;
  }

  .doc-date-range {
    width: 100%;

    // TDesign focuses both the outer popup reference and inner inputs, which
    // visually stacks into a "double border" — drop the inner shadow.
    :deep(.t-input--focused),
    :deep(.t-is-focused) {
      box-shadow: none;
    }
  }

  .doc-view-toggle {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    padding: 2px;
    background: var(--td-bg-color-secondarycontainer);
    border-radius: 6px;
    gap: 0;

    .doc-view-toggle-btn {
      width: 28px;
      height: 24px;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      border: 0;
      background: transparent;
      border-radius: 4px;
      color: var(--td-text-color-secondary, #888);
      cursor: pointer;
      transition: background-color 0.12s ease, color 0.12s ease;

      &:hover {
        color: var(--td-text-color-primary, #232323);
      }

      &.active {
        background: var(--td-bg-color-container, #fff);
        color: var(--td-brand-color, #0052d9);
        box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
      }
    }
  }

  :deep(.t-input) {
    font-size: 13px;
    background-color: var(--td-bg-color-secondarycontainer);
    border-color: transparent;
    border-radius: 6px;
    box-shadow: none !important;

    &:hover,
    &:focus,
    &.t-is-focused {
      border-color: var(--td-brand-color);
      background-color: var(--td-bg-color-container);
      box-shadow: none !important;
    }
  }

  :deep(.t-select) {
    .t-input {
      font-size: 13px;
      background-color: var(--td-bg-color-secondarycontainer);
      border-color: transparent;
      border-radius: 6px;
      box-shadow: none !important;

      &:hover,
      &.t-is-focused {
        border-color: var(--td-brand-color);
        background-color: var(--td-bg-color-container);
        box-shadow: none !important;
      }
    }
  }
}

.doc-scroll-container {
  position: relative;
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  padding-right: 4px;

  &.is-empty {
    display: flex;
    align-items: center;
    justify-content: center;
    overflow-y: hidden;
  }

  &.is-marquee-active {
    cursor: crosshair;
  }
}

.doc-marquee-box {
  position: absolute;
  z-index: 4;
  pointer-events: none;
  border: 1px solid var(--td-brand-color);
  background: color-mix(in srgb, var(--td-brand-color) 12%, transparent);
  border-radius: 2px;

  &.is-add {
    border-color: var(--td-brand-color);
    background: color-mix(in srgb, var(--td-brand-color) 14%, transparent);
  }

  &.is-subtract {
    border-color: var(--td-error-color-6);
    background: color-mix(in srgb, var(--td-error-color-6) 12%, transparent);
  }
}

/* 批量条悬浮在滚动区底部，不挤占列表高度 */
.doc-batch-bar-anchor {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 12px;
  z-index: 6;
  display: flex;
  justify-content: center;
  padding: 0 16px;
  pointer-events: none;

  &>* {
    pointer-events: auto;
  }
}

// Header 样式（无底部分割线，留更多空间给下方内容区）
.document-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  flex-shrink: 0;

  .document-header-title {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .document-title-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .kb-title-actions {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
    margin-left: 4px;
  }

  .document-breadcrumb {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
  }

  .breadcrumb-link {
    border: none;
    background: transparent;
    padding: 4px 8px;
    margin: -4px -8px;
    font: inherit;
    color: var(--td-text-color-secondary);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    border-radius: 6px;
    transition: all 0.12s ease;

    &:hover:not(:disabled) {
      color: var(--td-success-color);
      background: var(--td-bg-color-container);
    }

    &:disabled {
      cursor: not-allowed;
      color: var(--td-text-color-placeholder);
    }

    &.dropdown {
      padding-right: 6px;

      :deep(.t-icon) {
        font-size: 14px;
        transition: transform 0.12s ease;
      }

      &:hover:not(:disabled) {
        :deep(.t-icon) {
          transform: translateY(1px);
        }
      }
    }
  }

  .breadcrumb-separator {
    font-size: 14px;
    color: var(--td-text-color-placeholder);
  }

  .breadcrumb-current {
    color: var(--td-text-color-primary);
    font-weight: 600;
  }

  h2 {
    margin: 0;
    color: var(--td-text-color-primary);
    font-family: var(--app-font-family);
    font-size: 24px;
    font-weight: 600;
    line-height: 32px;
  }

  .document-subtitle {
    margin: 0;
    color: var(--td-text-color-placeholder);
    font-family: var(--app-font-family);
    font-size: 14px;
    font-weight: 400;
    line-height: 20px;
  }

  .parser-hint {
    display: flex;
    align-items: center;
    gap: 4px;
    margin: 2px 0 0;
    color: var(--td-warning-color);
    font-size: 12px;
    line-height: 1.4;
    cursor: pointer;
    transition: color 0.15s ease;

    &:hover {
      color: var(--td-warning-color-active);

      .parser-hint-link {
        text-decoration: underline;
      }
    }

    .parser-hint-icon {
      font-size: 12px;
      flex-shrink: 0;
    }

    .parser-hint-link {
      color: var(--td-brand-color);
      margin-left: 2px;
      white-space: nowrap;
    }
  }

  .storage-engine-warning {
    display: flex;
    align-items: center;
    gap: 4px;
    margin: 2px 0 0;
    color: var(--td-warning-color);
    font-size: 12px;
    line-height: 1.4;
    cursor: pointer;
    transition: color 0.15s ease;

    &:hover {
      color: var(--td-warning-color-active);

      .warning-link {
        text-decoration: underline;
      }
    }

    .warning-icon {
      font-size: 12px;
      flex-shrink: 0;
    }

    .warning-link {
      color: var(--td-brand-color);
      margin-left: 2px;
      white-space: nowrap;
    }
  }
}


.document-upload-input {
  display: none;
}

.kb-settings-button {
  width: 30px;
  height: 30px;
  border: none;
  border-radius: 50%;
  background: var(--td-bg-color-secondarycontainer);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  transition: all 0.2s ease;
  padding: 0;

  &:hover:not(:disabled) {
    background: var(--td-success-color-light);
    color: var(--td-brand-color);
    box-shadow: none;
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.4;
  }

  :deep(.t-icon) {
    font-size: 18px;
  }
}

.tag-filter-bar {
  display: flex;
  align-items: center;
  gap: 16px;

  .tag-filter-label {
    color: var(--td-text-color-placeholder);
    font-size: 14px;
  }
}

.card-tag-selector {
  display: flex;
  align-items: center;

  .card-tag-chips {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex-wrap: nowrap;
    cursor: pointer;
  }

  .card-tag-overflow {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: 18px;
    min-width: 18px;
    padding: 0 5px;
    border-radius: 999px;
    border: 1px solid var(--td-component-stroke);
    color: var(--td-text-color-placeholder);
    font-size: 10px;
    line-height: 1;
    cursor: pointer;
    transition: all 0.2s ease;

    &:hover {
      border-color: var(--td-brand-color);
      color: var(--td-brand-color);
      background: var(--td-bg-color-secondarycontainer);
    }
  }

  :deep(.t-tag) {
    cursor: pointer;
    max-width: 120px;
    height: 18px;
    line-height: 18px;
    border-radius: 999px;
    border-color: var(--td-component-stroke);
    color: var(--td-text-color-secondary);
    padding: 0 6px;
    background: transparent;
    transition: all 0.2s ease;

    &:hover {
      border-color: var(--td-brand-color);
      color: var(--td-brand-color-active);
      background: var(--td-bg-color-secondarycontainer);
    }
  }

  .tag-text {
    display: inline-block;
    max-width: 80px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    vertical-align: middle;
    font-size: 11px;
  }

  .card-tag-add {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    height: 18px;
    padding: 0 6px;
    border-radius: 999px;
    border: 1px dashed var(--td-component-stroke);
    color: var(--td-text-color-placeholder);
    font-size: 11px;
    cursor: pointer;
    transition: all 0.2s ease;

    .t-icon {
      font-size: 12px;
    }

    &:hover {
      border-color: var(--td-brand-color);
      color: var(--td-brand-color-active);
      background: var(--td-bg-color-secondarycontainer);
      border-style: solid;
    }
  }
}


.card-bottom-right {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  overflow: hidden;
}

.faq-manager-wrapper {
  flex: 1;
  min-height: 0;
  padding: 24px 32px;
  overflow-y: auto;
  margin: 0 16px 0 4px;
}

@media (max-width: 1250px) and (min-width: 1045px) {
  .answers-input {
    transform: translateX(-329px);
  }

  :deep(.t-textarea__inner) {
    width: 654px !important;
  }
}

@media (max-width: 1045px) {
  .answers-input {
    transform: translateX(-250px);
  }

  :deep(.t-textarea__inner) {
    width: 500px !important;
  }
}

@media (max-width: 750px) {
  .answers-input {
    transform: translateX(-182px);
  }

  :deep(.t-textarea__inner) {
    width: 340px !important;
  }
}

@media (max-width: 600px) {
  .answers-input {
    transform: translateX(-164px);
  }

  :deep(.t-textarea__inner) {
    width: 300px !important;
  }
}

@keyframes contentFadeIn {
  from {
    opacity: 0;
    transform: translateY(6px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.doc-card-list {
  box-sizing: border-box;
  display: grid;
  // 文档卡片信息量较大（标题 + 摘要 + 标签/类型），保持稍宽的最小列宽，避免一行塞太多导致内容拥挤。
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 12px;
  align-content: flex-start;
  width: 100%;

  // When the folder-card-list wrapper is empty (no folders), collapse
  // so it doesn't add extra gap in the parent flex.
  &:empty {
    display: none;
  }

  &.doc-card-list-animated {
    animation: contentFadeIn 0.32s ease-out;
  }
}


.knowledge-card-skeleton {
  cursor: default;

  .card-content {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 10px 14px 8px;
  }

  .card-content-nav {
    margin-bottom: 8px;
  }

  .card-bottom {
    flex-shrink: 0;
    margin-top: auto;
    width: 100%;
    padding: 0 14px;
    box-sizing: border-box;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    border-top: 1px solid var(--td-component-stroke);
  }
}

.doc-empty-state {
  flex: 1;
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  min-height: 100%;
}


.card-menu {
  display: flex;
  flex-direction: column;
  min-width: 140px;
  gap: 1px;
}

.card-menu-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  cursor: pointer;
  color: var(--td-text-color-primary);
  transition: all 0.15s cubic-bezier(0.2, 0, 0, 1);
  border-radius: 6px;
  font-size: 14px;
  line-height: 20px;

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &:active {
    background: var(--td-bg-color-container-active);
    transform: scale(0.98);
  }

  .icon {
    font-size: 16px;
    color: var(--td-text-color-secondary);
    transition: all 0.15s cubic-bezier(0.2, 0, 0, 1);
  }

  &:hover .icon {
    color: var(--td-text-color-primary);
  }

  &.danger {
    color: var(--td-error-color-6);
    margin-top: 4px;
    position: relative;

    &::before {
      content: '';
      position: absolute;
      top: -3px;
      left: 8px;
      right: 8px;
      height: 1px;
      background: var(--td-component-stroke);
    }

    .icon {
      color: var(--td-error-color-6);
    }

    &:hover {
      background: var(--td-error-color-1);
      color: var(--td-error-color-6);

      .icon {
        color: var(--td-error-color-6);
      }
    }

    &:active {
      background: var(--td-error-color-2);
    }
  }
}

.move-menu {
  min-width: 220px;
  max-width: 280px;
  max-height: 360px;
  overflow-y: auto;

  .move-menu-header {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 12px;
    font-size: 13px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    border-bottom: 1px solid var(--td-component-stroke);
    cursor: pointer;

    &:hover {
      background: var(--td-bg-color-container-hover);
    }
  }

  .move-menu-loading {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 20px 0;
  }

  .move-menu-empty {
    padding: 12px 16px;
    font-size: 12px;
    color: var(--td-text-color-placeholder);
    text-align: center;
    line-height: 1.5;
  }

  .move-target-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .move-target-count {
    font-size: 12px;
    color: var(--td-text-color-placeholder);
  }

  .move-confirm-body {
    padding: 8px;

    .move-target-info {
      display: flex;
      align-items: center;
      gap: 6px;
      padding: 6px 8px;
      background: var(--td-bg-color-container-hover);
      border-radius: 6px;
      font-size: 13px;
      color: var(--td-text-color-secondary);
      margin-bottom: 8px;
    }

    .move-mode-item {
      display: flex;
      align-items: flex-start;
      gap: 6px;
      padding: 6px 8px;
      border-radius: 6px;
      cursor: pointer;
      margin-bottom: 4px;

      &:hover {
        background: var(--td-bg-color-container-hover);
      }

      &.active {
        background: var(--td-brand-color-light);
      }

      .move-mode-text {
        display: flex;
        flex-direction: column;
        gap: 2px;

        .move-mode-label {
          font-size: 13px;
          font-weight: 500;
          color: var(--td-text-color-primary);
        }

        .move-mode-desc {
          font-size: 11px;
          color: var(--td-text-color-placeholder);
          line-height: 1.4;
        }
      }
    }

    .move-confirm-actions {
      display: flex;
      justify-content: flex-end;
      gap: 8px;
      margin-top: 8px;
    }
  }
}

.card-draft {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  flex-shrink: 0;
}

.card-draft-tip {
  color: var(--td-warning-color);
  font-size: 11px;
}

.knowledge-card {
  min-width: 240px;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--td-component-border);
  height: 136px;
  border-radius: 8px;
  overflow: hidden;
  box-sizing: border-box;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
  background: var(--td-bg-color-container);
  position: relative;
  cursor: pointer;
  transition: border-color 0.2s ease, box-shadow 0.2s ease, background-color 0.2s ease;

  /* 仅在批量管理模式下渲染 checkbox，常态下不占位，避免标题在 hover 时右滑 */
  .card-nav-check {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 29px;
    margin-right: 8px;
    cursor: pointer;

    .card-select-checkbox {
      margin: 0;
      line-height: 0;

      :deep(.t-checkbox) {
        align-items: center;
      }

      :deep(.t-checkbox__label) {
        display: none !important;
        width: 0 !important;
        min-width: 0 !important;
        margin: 0 !important;
        padding: 0 !important;
      }

      :deep(.t-checkbox__input) {
        margin: 0;
      }

      :deep(.t-checkbox__input-wrapper) {
        margin: 0;
      }
    }
  }

  .card-content {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 10px 14px 8px;
  }

  .card-analyze {
    flex-shrink: 0;
    height: 52px;
    display: flex;
    align-items: flex-start;
  }

  .card-analyze-loading {
    display: block;
    color: var(--td-brand-color);
    font-size: 14px;
    margin-top: 2px;
  }

  .card-analyze-txt {
    color: var(--td-brand-color);
    font-family: var(--app-font-family);
    font-size: 11px;
    margin-left: 8px;
  }

  // In-flight / failed: only status text + trace icon open the drawer.
  .card-analyze-trace {
    height: auto;
    min-height: 0;
    align-items: center;
    gap: 2px;
  }

  .card-analyze-trace-link {
    cursor: pointer;

    &:hover {
      text-decoration: underline;
    }
  }

  .card-analyze-trace-btn {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    margin: 0;
    padding: 2px;
    border: none;
    background: transparent;
    color: var(--td-brand-color);
    cursor: pointer;
    line-height: 1;
    border-radius: 4px;

    :deep(.t-icon) {
      font-size: 14px;
    }

    &:hover {
      background: var(--td-bg-color-component-hover);
    }
  }

  .card-analyze.failure .card-analyze-trace-btn {
    color: var(--td-error-color);
  }

  .failure {
    color: var(--td-error-color);
  }

  .card-content-nav {
    flex-shrink: 0;
    display: flex;
    align-items: flex-start;
    gap: 0;
    margin-bottom: 6px;
  }

  .card-content-title {
    flex: 1;
    min-width: 0;
    height: 24px;
    line-height: 24px;
    display: inline-block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--td-text-color-primary);
    font-family: var(--app-font-family);
    font-size: 14px;
    font-weight: 600;
    letter-spacing: 0.01em;
    margin-right: 8px;
  }

  .more-wrap {
    flex-shrink: 0;
    display: flex;
    width: 25px;
    height: 25px;
    justify-content: center;
    align-items: center;
    border-radius: 5px;
    cursor: pointer;
  }

  .more-wrap:hover {
    background: var(--td-component-stroke);
  }

  .more-icon {
    width: 14px;
    height: 14px;
  }

  .active-more {
    background: var(--td-component-stroke);
  }

  .card-content-txt {
    flex: 1;
    min-height: 0;
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    overflow: hidden;
    color: var(--td-text-color-secondary);
    font-family: var(--app-font-family);
    font-size: 12px;
    font-weight: 400;
    line-height: 19px;
  }

  .card-bottom {
    flex-shrink: 0;
    margin-top: auto;
    padding: 0 14px;
    box-sizing: border-box;
    height: 32px;
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    background: var(--td-bg-color-container);
    border-top: 1px solid var(--td-component-stroke);
  }

  .card-time {
    flex-shrink: 0;
    color: var(--td-text-color-secondary);
    font-family: var(--app-font-family);
    font-size: 12px;
    font-weight: 400;
    white-space: nowrap;
  }

  .card-type {
    flex-shrink: 0;
    color: var(--td-text-color-placeholder);
    font-family: var(--app-font-family);
    font-size: 11px;
    font-weight: 500;
    padding: 0;
    background: transparent;
    letter-spacing: 0.02em;
  }
}

.card-bottom-right {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  overflow: hidden;
}

.knowledge-card:hover {
  border-color: color-mix(in srgb, var(--td-component-stroke) 55%, var(--td-brand-color));
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.07);
}

/* 悬停知识卡片时跟随鼠标的详情气泡 */
.knowledge-card-hover-popover {
  position: fixed;
  z-index: 9999;
  pointer-events: none;
  min-width: 220px;
  max-width: 360px;
  padding: 12px 14px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
  font-family: var(--app-font-family);
  transition: opacity 0.15s ease;
  will-change: transform;

  /* 防止气泡内容抖动 */
  backface-visibility: hidden;
  -webkit-backface-visibility: hidden;
  transform: translateZ(0);
  -webkit-transform: translateZ(0);

  .card-popover-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--td-text-color-primary);
    margin-bottom: 8px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .card-popover-status {
    font-size: 12px;
    margin-bottom: 6px;
    display: flex;
    align-items: center;
    gap: 6px;

    &.parsing {
      color: var(--td-brand-color);
    }

    &.failure {
      color: var(--td-error-color);
    }

    &.draft {
      color: var(--td-warning-color);
    }
  }

  .card-popover-desc {
    font-size: 12px;
    color: var(--td-text-color-secondary);
    line-height: 1.5;
    margin-bottom: 8px;
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 5;
    line-clamp: 5;
    overflow: hidden;
  }

  .card-popover-error-msg {
    display: block;
    margin-top: 4px;
    font-size: 11px;
    color: var(--td-error-color);
    opacity: 0.95;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 280px;
  }

  .card-popover-source {
    font-size: 11px;
    color: var(--td-brand-color);
    margin-bottom: 6px;
    display: flex;
    align-items: center;
    gap: 4px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }

  .card-popover-folder {
    font-size: 11px;
    color: var(--td-text-color-secondary);
    margin-bottom: 6px;
    display: flex;
    align-items: center;
    gap: 4px;
    overflow: hidden;
    white-space: nowrap;

    .card-popover-folder-label {
      flex-shrink: 0;
    }

    .card-popover-folder-path {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }

  .card-popover-extra {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    font-size: 11px;
    color: var(--td-text-color-secondary);
    margin-bottom: 6px;
  }

  .card-popover-created,
  .card-popover-size {
    flex-shrink: 0;
  }

  .card-popover-meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    font-size: 11px;
    color: var(--td-text-color-secondary);
  }

  .card-popover-channel {
    padding: 1px 6px;
    background: var(--td-warning-color-light);
    color: var(--td-warning-color);
    border-radius: 4px;
  }

  .card-popover-tags {
    display: inline-flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px;
    max-width: 100%;
  }

  .card-popover-tag-chip {
    max-width: 120px;
    height: 18px;
    line-height: 18px;
    border-radius: 999px;
    border-color: var(--td-component-stroke);
    color: var(--td-text-color-secondary);
    padding: 0 6px;
    background: transparent;

    .tag-text {
      display: inline-block;
      max-width: 80px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      vertical-align: middle;
      font-size: 11px;
    }
  }

  .card-popover-type {
    padding: 1px 6px;
    background: var(--td-bg-color-secondarycontainer);
    color: var(--td-text-color-secondary);
    border-radius: 4px;
  }

  .card-popover-hint {
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--td-component-stroke);
    font-size: 11px;
    color: var(--td-text-color-secondary);
  }
}

.url-import-form {
  padding: 8px 0;

  .url-input-label {
    color: var(--td-text-color-primary);
    font-size: 14px;
    font-weight: 500;
    margin-bottom: 8px;
  }

  .url-input-tip {
    color: var(--td-text-color-secondary);
    font-size: 12px;
    margin-top: 8px;
    line-height: 1.5;
  }
}

.knowledge-card-upload {
  color: var(--td-text-color-primary);
  font-family: var(--app-font-family);
  font-size: 14px;
  font-weight: 400;
  cursor: pointer;

  .btn-upload {
    margin: 33px auto 0;
    width: 112px;
    height: 32px;
    border: 1px solid var(--td-component-border);
    display: flex;
    justify-content: center;
    align-items: center;
    margin-bottom: 24px;
  }

  .svg-icon-download {
    margin-right: 8px;
  }
}

.upload-described {
  color: var(--td-text-color-disabled);
  font-family: var(--app-font-family);
  font-size: 12px;
  font-weight: 400;
  text-align: center;
  display: block;
  width: 188px;
  margin: 0 auto;
}

.del-card {
  vertical-align: middle;
}

/* ---- Folder breadcrumb bar ---- */
.folder-breadcrumb-bar {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px 16px;
  margin: 0 0 4px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 6px;
  font-size: 13px;
  overflow: hidden;
  white-space: nowrap;
  transition: margin 0.15s ease, padding 0.15s ease;
}

.folder-breadcrumb-icon {
  font-size: 16px;
  color: var(--td-warning-color);
  margin-right: 4px;
  flex-shrink: 0;
}

.folder-breadcrumb-link {
  border: none;
  background: transparent;
  padding: 2px 6px;
  font-size: 13px;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  border-radius: 4px;
  transition: all 0.12s ease;

  &:hover {
    color: var(--td-brand-color);
    background: var(--td-bg-color-container-hover);
  }

  &.current {
    color: var(--td-text-color-primary);
    font-weight: 600;
  }
}

.folder-breadcrumb-sep {
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  flex-shrink: 0;
}

/* ---- Folder card in grid ---- */
.knowledge-card--folder {
  cursor: pointer;
  .card-content-nav {
    gap: 6px;
  }
  .folder-card-icon {
    color: var(--td-warning-color);
    flex-shrink: 0;
  }
  .folder-card-more {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--td-text-color-placeholder);
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.12s, color 0.12s;
    flex-shrink: 0;
    margin-left: auto;
    &:hover {
      color: var(--td-brand-color);
      background: var(--td-brand-color-light);
    }
  }
  &:hover .folder-card-more {
    opacity: 1;
  }
  &:hover {
    border-color: var(--td-warning-color);
  }
}

/* Folder card ⋯ menu — aligned with document .card-menu / .doc-action-menu-item */
.folder-card-menu {
  display: flex;
  flex-direction: column;
  min-width: 140px;
  gap: 1px;
}
.folder-card-menu-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  cursor: pointer;
  color: var(--td-text-color-primary);
  transition: all 0.15s cubic-bezier(0.2, 0, 0, 1);
  border-radius: 6px;
  font-size: 14px;
  line-height: 20px;

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &:active {
    background: var(--td-bg-color-container-active);
    transform: scale(0.98);
  }

  .t-icon {
    font-size: 16px;
    color: var(--td-text-color-secondary);
    transition: color 0.15s ease;
  }

  &:hover .t-icon {
    color: var(--td-text-color-primary);
  }

  &.danger {
    color: var(--td-error-color-6);
    margin-top: 4px;
    position: relative;

    &::before {
      content: '';
      position: absolute;
      top: -3px;
      left: 8px;
      right: 8px;
      height: 1px;
      background: var(--td-component-stroke);
    }

    .t-icon {
      color: var(--td-error-color-6);
    }

    &:hover {
      background: var(--td-error-color-1);
      color: var(--td-error-color-6);

      .t-icon {
        color: var(--td-error-color-6);
      }
    }

    &:active {
      background: var(--td-error-color-2);
    }
  }
}
</style>

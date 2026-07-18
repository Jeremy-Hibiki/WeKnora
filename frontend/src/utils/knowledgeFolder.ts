import type { KnowledgeFolder } from '@/types/knowledgeFolder';

/**
 * Collect a folder and all of its descendant folder IDs from a folder tree.
 *
 * Used when moving folders to prevent selecting a target folder that is the
 * source folder itself or any of its descendants (which would create a cycle
 * or be rejected by the API anyway).
 */
export function getFolderDescendantIds(
  folderTree: KnowledgeFolder[],
  folderIds: string[],
): string[] {
  const targetIds = new Set<string>(folderIds);
  const result = new Set<string>();

  function collectChildren(node: KnowledgeFolder) {
    result.add(node.id);
    if (node.children?.length) {
      for (const child of node.children) {
        collectChildren(child);
      }
    }
  }

  function walk(nodes: KnowledgeFolder[]) {
    for (const node of nodes) {
      if (targetIds.has(node.id)) {
        collectChildren(node);
      } else if (node.children?.length) {
        walk(node.children);
      }
    }
  }

  walk(folderTree);
  return [...result];
}

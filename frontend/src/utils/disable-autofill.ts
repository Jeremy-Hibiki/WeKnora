/**
 * Globally discourage browser/password-manager autofill on app inputs.
 * Skips elements that already declare autocomplete (e.g. login email).
 *
 * Performance: the global MutationObserver batches added-node processing
 * with requestAnimationFrame so a burst of DOM mutations (e.g. opening a
 * modal that teleports dozens of nodes) collapses into a single guard
 * pass instead of running querySelectorAll synchronously on every
 * mutation record. Without batching the observer made the settings
 * dialog and other large overlays feel janky.
 */
function guardElement(el: HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement) {
 const explicit = el.getAttribute('autocomplete');
 if (explicit !== null && explicit !== '') return;
 if (el.closest('[data-allow-autofill]')) return;

 if (el instanceof HTMLInputElement && el.type === 'password') {
  el.setAttribute('autocomplete', 'new-password');
 } else {
  el.setAttribute('autocomplete', 'off');
 }
}

/**
 * Returns true if `root` itself or any of its descendants could need
 * guarding — i.e. it is / contains an input, textarea, select, or form.
 * Used as a cheap pre-filter so we never call the more expensive
 * querySelectorAll on irrelevant subtrees (text nodes, svgs, etc.).
 */
function mayContainFields(root: HTMLElement): boolean {
 if (
  root instanceof HTMLInputElement ||
  root instanceof HTMLTextAreaElement ||
  root instanceof HTMLSelectElement ||
  root instanceof HTMLFormElement
 ) {
  return true;
 }
 // A single selector query with a union is cheaper than running four
 // separate querySelectorAll calls downstream on nodes that have none.
 return root.querySelector('input, textarea, select, form') !== null;
}

function guardTree(root: HTMLElement) {
 if (!mayContainFields(root)) return;

 if (root instanceof HTMLInputElement || root instanceof HTMLTextAreaElement || root instanceof HTMLSelectElement) {
  guardElement(root);
 }

 root.querySelectorAll('input, textarea, select').forEach((node) => {
  guardElement(node as HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement);
 });

 root.querySelectorAll('form').forEach((form) => {
  if (!form.hasAttribute('autocomplete')) {
   form.setAttribute('autocomplete', 'off');
  }
 });
}

let installed = false;

export function installAutofillGuard() {
 if (installed || typeof document === 'undefined') return;
 installed = true;

 // Guard whatever already exists at install time.
 guardTree(document.body);

 // Batch DOM mutations into a single rAF tick. Without this, every
 // individual mutation record runs querySelectorAll synchronously — a
 // modal that injects 100 nodes fires 100 callbacks, each scanning the
 // added subtree, which makes large overlays (settings, drawers) feel
 // janky during open / typing / section switches.
 const pending = new Set<HTMLElement>();
 let scheduled = false;

 const flush = () => {
  scheduled = false;
  const batch = pending;
  pending.clear();
  for (const node of batch) {
   // Skip nodes detached between the mutation and the flush —
   // Vue/TDesign can add-then-remove within the same tick.
   if (node.isConnected) {
    guardTree(node);
   }
  }
 };

 const schedule = () => {
  if (!scheduled) {
   scheduled = true;
   requestAnimationFrame(flush);
  }
 };

 const observer = new MutationObserver((mutations) => {
  for (const mutation of mutations) {
   for (const node of mutation.addedNodes) {
    if (node instanceof HTMLElement) {
     pending.add(node);
    }
   }
  }
  if (pending.size > 0) {
   schedule();
  }
 });

 observer.observe(document.body, { childList: true, subtree: true });
}

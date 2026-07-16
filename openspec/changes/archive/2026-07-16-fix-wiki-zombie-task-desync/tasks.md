## 1. Gap ② — Wiki worker skips failed knowledge

- [x] 1.1 Add `types.ParseStatusFailed` to the `switch` in `isKnowledgeGone` (`internal/application/service/wiki_ingest.go:~2362`)
- [x] 1.2 Update the `isKnowledgeGone` docstring to cover `failed` status
- [x] 1.3 Add test: wiki worker claims op for `ParseStatusFailed` knowledge → `mapOneDocument` returns nil skip, `finalizeWikiSubtask` drains the slot
- [x] 1.4 Add test: `isKnowledgeGone` returns false for `ParseStatusCompleted` and `ParseStatusFinalizing` (ensure no over-blocking)
- [x] 1.5 Verify existing wiki ingest tests still pass

## 2. Gap ① — Housekeeping sweep scrubs wiki ops

- [x] 2.1 Inject `knowledgeService` (or a minimal `scrubWikiPendingIngest`-equivalent) into `HousekeepingService` — currently it only has `db` + `inspector`
- [x] 2.2 After the bulk `UPDATE ... SET parse_status=failed` in `runSweep`, iterate `stuckIDs` and call `scrubWikiPendingIngest(ctx, kbID, kid, "housekeeping")` for each
- [x] 2.3 Ensure scrub failure is logged and non-fatal (does not abort the sweep)
- [x] 2.4 Add test: housekeeping marks knowledge failed → wiki ingest op removed from `task_pending_ops`, retract op preserved
- [x] 2.5 Add test: scrub DB error → warning logged, sweep continues for remaining rows
- [x] 2.6 Verify existing housekeeping tests still pass

## 3. Gap ③ — Failure-path detached context

- [x] 3.1 In `trimPendingList` (`wiki_ingest.go:923`), wrap `pendingRepo.DeleteByIDs` call with `context.WithTimeout(context.WithoutCancel(ctx), finalizeSubtaskDetachedTimeout)`
- [x] 3.2 In `requeueFailedOps` (`wiki_ingest.go:966`), wrap `IncrFailCount`, `ReleaseByIDs`, `deadLetterRepo.Insert`, and `DeleteByIDs` calls with detached+timeout context
- [x] 3.3 Add test: `requeueFailedOps` with cancelled ctx → `IncrFailCount` still succeeds, fail_count increments
- [x] 3.4 Add test: `trimPendingList` with cancelled ctx → `DeleteByIDs` still succeeds, rows removed
- [x] 3.5 Verify existing wiki ingest batch tests still pass

## 4. Integration & verification

- [x] 4.1 Run full test suite for affected packages: `go test -count=1 ./internal/application/service/ ./internal/container/`
- [x] 4.2 Run `go vet ./internal/application/service/ ./internal/container/`
- [x] 4.3 Verify no regressions in the wiki ingest → finalize → dead-letter lifecycle by reviewing test output for the full flow

# Forum capability matrix

## Decision status

This matrix applies only to the local, generated-data Discourse fixture locked to **Discourse v2026.8.0**, Git commit **`badad7b0456a628e578bc48b9f8c1259422b5d58`**, with `discourse-reactions` from that tree and the local `credis-event-bridge` plugin.

- **Local event-sync gate:** **PASS** for the covered interaction/bookmark rows on the fixed generated-data instance. The complete off→on→restart→off→re-enable evidence passed; the bridge event is the sole candidate accounting source for covered interaction/bookmark relationships. Core Webhooks are verification leads only.
- **Automatic scoring / production:** **No-Go**. Arbitrary manual SQL cannot be universally detected without forbidden Core-table triggers. Known callback-bypassing application maintenance must use `CredisEventBridge::MaintenanceGuard`; raw SQL is operationally prohibited. Any unguarded SQL exposure makes global completeness uncertifiable.
- **Gamification write terminality:** **Blocked**. No P0 rule may write points from these candidates until a separate ledger/reconciliation/production approval gate passes.

## Candidate source contract

Every delivered bridge event contains only these immutable business fields:

`event_uid`, `epoch_id`, `relation_kind`, `actor_user_id`, `target_type`, `target_id`, `revision`, `operation_id`, `from_state`, `to_state`, `occurred_at`, `origin`.

- `event_uid`: globally unique UUID, stable across retry, push, pull, ACK, and history replay.
- Source version: monotonic `revision` per complete relation key `(relation_kind, actor_user_id, target_type, target_id)`. No state change means no revision.
- `occurred_at`: database timestamp written atomically with the source mutation and outbox row; serialized at microsecond precision.
- Inverse: represented by the same relation key and the next revision (`ordinary_like→none`, `active→inactive`, or `reaction:*→none`).
- Delivery: signed push requires an exact durable ACK UID set. Signed pull is ordered by committed `outbox_id`, bounded to 1–100 rows. Signed history is epoch- and time-bound (maximum 31 days), 1–100 rows/page, snapshot-stable for one hour.
- Permissions: mutation authorization remains the locked Discourse route/service authorization. Integration read/ACK/history uses HMAC request authentication, not an administrator browser session. Baseline access is plugin database scope over all source rows and records that scope explicitly.

## Covered relation rows

| Capability | Canonical source and transition | UID / source version / occurred_at | Relation key | Permission and pagination | Inverse and completeness | Decision |
|---|---|---|---|---|---|---|
| Ordinary Like | Active Core `PostAction` Like; `none→ordinary_like` | Bridge `event_uid`; per-key `revision`; transactional DB `occurred_at` | `interaction`, actor user, `Post`, post ID | Core Like authorization; baseline keyset pages over active Like rows | Unlike is `ordinary_like→none`; Like/unlike/re-Like required. Certified only inside a complete collecting epoch | Candidate accounting source in local gate |
| Ordinary Unlike | Locked `PostAction#remove_act!`; canonical state re-read after source change | Same immutable bridge fields; next relation revision | Same interaction key | Core undo authorization/window; no separate webhook authority | If a `ReactionUser` still exists, shadow-Like removal is **not** an inverse and emits no cancellation | Candidate accounting source in local gate |
| Bookmark add | Core `Bookmark` create for `bookmarkable_type=Post`; `inactive→active` | Bridge UID; per-bookmark relation revision; transactional timestamp | `bookmark`, actor user, `Post`, post ID | Logged-in bookmark authorization; baseline pages only Post bookmarks | Remove is `active→inactive`; add/remove/add required | Candidate accounting source in local gate |
| Bookmark remove | Core `Bookmark` destroy; `active→inactive` | Same immutable bridge fields; next relation revision | Same bookmark key | Core bookmark ownership; callback and outbox share source transaction | `delete_all` bypass is not auto-detected; it must run inside the maintenance guard, which invalidates the epoch first | Candidate only for guarded normal path |
| Reaction none→A | `DiscourseReactions::ReactionManager#toggle!` and direct `ReactionUser` create. A persisted `ReactionUser` always has precedence and canonicalizes to `reaction:<value>`, including when its value is the configured main reaction; `ordinary_like` applies only when an active Core Like exists without a `ReactionUser` | One bridge UID/operation ID for the user operation; next interaction revision; transactional timestamp | Same interaction key as ordinary Like | Reactions Guardian authorization; baseline pages `ReactionUser` joined to reaction value | A→none is the inverse. Nested shadow-Like callbacks are suppressed | Candidate accounting source in local gate |
| Reaction A→B | One ReactionManager operation re-read from persisted `ReactionUser` | One UID and one revision for the complete switch, never half-events | Same interaction key | Same authorization; consumer pagination as above | Old reaction→new reaction is one indivisible transition | Candidate accounting source in local gate |
| Main↔non-main Reaction | Actual `PostAction`/`ReactionUser` state determines `ordinary_like`, `reaction:<value>`, or `none` | One UID/revision per canonical change | Same interaction key | Same authorization | Main→non-main→main required; no independent shadow reward | Candidate accounting source in local gate |
| Cross-interface Like→Reaction→Unlike | Ordinary Like endpoint, Reactions endpoint, ordinary Unlike endpoint | One chain on the same relation; operation IDs remain per source operation | Same interaction key | Each Core/plugin route keeps its own authorization | Ordinary Unlike that deletes only a shadow Like leaves Reaction state and emits no false inverse | Candidate accounting source in local gate |
| Reaction Like synchronizer | Official `ReactionLikeSynchronizer` may create/restore/delete shadow `PostAction` rows | **No new bridge UID or revision** when canonical `ReactionUser` state is unchanged | Existing interaction key | Operator-only maintenance invocation | Included and excluded reaction fixtures must retain identical bridge event/revision sets before/after sync | Verification requirement; not a separate source |

## Epoch and baseline completeness

A process load never auto-certifies capture. `EpochLifecycle.start!` holds the exclusive capture lock, verifies the exact source build/hooks, and scans bounded pages of:

1. active PostAction Likes;
2. ReactionUsers joined to reaction values;
3. Post bookmarks; and
4. existing bridge relations, so inactive/none states are reset at a new boundary.

It persists a canonical manifest and SHA-256 digest over the canonical rows, exact compatibility token, explicit source/target/permission scope, page and row bounds, source counts and maximum ID/`updated_at` watermarks, and every behavior-relevant Reactions setting (enabled flag/codes, configured main reaction, excluded-from-like set, Like synchronization, and allow-any-emoji). Activation revalidates the manifest against current hooks/settings; a later mismatch invalidates collection. Baseline rows are state evidence only: they do not create events or retroactive points.

- Controlled disable closes the collecting epoch before writes and opens an explicit incomplete gap interval.
- Hook/source mismatch, outbox atomic failure, uncontrolled restart, or guarded maintenance marks the collecting epoch `incomplete`, sets `baseline_complete=false`, and records relationship/time quarantine scope.
- Re-enable creates a new epoch and complete baseline. An old incomplete interval is never changed back to complete, even when current REST state or relation revision appears continuous.
- `delivery_degraded` changes only delivery metadata/freshness; it does not change source epoch status or baseline evidence.

## Uncovered and blocked rows

| Existing source | Current evidence problem | Status |
|---|---|---|
| Core topic/post lifecycle and `topic_edited` webhook | Webhook is a verification lead but lacks the locked per-relation revision/completeness contract required for accounting | `pending_review` |
| Solved / accepted answer | Existing event does not yet prove durable source version, inverse, and complete capture window | `pending_review` |
| Badge grant/revoke | No approved versioned inverse/completeness contract | `pending_review` |
| Other Core Webhooks | Delivery observation does not prove source completeness or A→B→A absence | `pending_review` |
| Known callback-bypassing application maintenance | Exact locked guards run before `UserDestroyer#destroy`, direct User destruction, `PostBookmarkable.cleanup_deleted`, `PostMover#to_topic`/`to_new_topic`, and `DestroyTask#destroy_stats`; each invalidates/quarantines before covered bulk SQL/copy/delete behavior | Supported only through installed exact wrappers/callback; missing guard blocks certification |
| Imports or unknown bulk application paths | No approved wrapper/evidence contract | `pending_review`; must not run while locally certified |
| Arbitrary direct SQL | No compliant universal detector exists without forbidden Core-table changes | **Uncertifiable; production No-Go** |
| Credis Gamification point write | Ledger, reconciliation, source approval, and terminality gates remain unresolved | **Blocked** |

## Redacted fixed-instance evidence

Task 4 used only generated data on the locked local instance. The initial plugin-off matrix made 25 real requests over seven posts and left the outbox `0→0`. The enabled matrix produced 17 canonical events with 17 unique UIDs; `MATRIX` manifest digest `d4cbb475f689c12b36209a0347d2418ac0030fd6c80bd3b671a9853135a1009d`; the behavior-setting change produced distinct `POST_SETTING` digest `b84195c319eb57ba538d8767fe1ff8375499fd6484593a9ce709a55d6d3e13e5`; synchronizer event delta `0`; the actual Post bookmark cleanup path invalidated that epoch with `known_maintenance_bypass:post_bookmark_cleanup`; 17 events were durably pushed/ACKed; 18 rows replayed through signed history; and `LATE_LOWER` remained recoverable after `EARLY_HIGHER` committed and was ACKed first. Uncontrolled restart left `PRE_RESTART` incomplete and created complete `POST_RESTART`; controlled disable created `CONTROLLED_GAP`. The final plugin-off contrast left the outbox `20→20`; re-enable created complete `POST_GAP` with manifest digest `a324ff15160e38bbc9f29f40567c0d13b32ba46acbf7aaf3579c6b67965e2dee`, emitted zero retroactive events, and preserved `CONTROLLED_GAP` as incomplete.

The controlled test record states that no arbitrary Core interaction SQL mutation occurred. SQL was used only for test-key cleanup and schema/evidence inspection. All ephemeral keys were revoked and deleted, and no collecting epoch remained at cleanup. Raw bodies and generated identifiers remain private and are not part of this document.

**Always redacted from committed docs/review artifacts:** authorization headers, HMAC signatures and secret material, API keys, request bodies containing identifiers, and literal generated user/post/event/epoch/outbox IDs. **Allowed redacted evidence:** aggregate request/event/replay counts, zero/nonzero deltas, state/revision ordering expressed with stable labels, version/source-file digests, and baseline manifest digests.

## Locked compatibility evidence

The bridge verifies these source files before capture and persists the combined compatibility token:

- `lib/post_action_creator.rb` SHA-256 `ad82cb99ec5453a060c7012b859be71f67fbd5d2c325596923d5b3e35034811d`
- `app/models/post_action.rb` SHA-256 `cfb73b8cd7f32cf0845211e722ee6eb4f2fb17962cba463212bbee89f8997bc5`
- `plugins/discourse-reactions/app/services/discourse_reactions/reaction_manager.rb` SHA-256 `cca5a5ed5ed80ff48e95684de5b8d1104343da9e883f5d28f2c74e37846f7130`
- `app/services/user_destroyer.rb` SHA-256 `ec88343a1144197ac395b7998e91d9bb78d8675f7bfeb695814984a5b4b8cd85`
- `app/models/post_mover.rb` SHA-256 `da171aac287b7899c05f4519fd9171471832673ccf9c2299bd00c092e55cf428`
- `app/services/destroy_task.rb` SHA-256 `0b8628c074c7d579a8756e004a38cc3bf985fefbc384f772d5dbab037a4172cd`
- `app/services/post_bookmarkable.rb` SHA-256 `9fa64569afff1dc666d9115158a2eed0813a59448921186242187505929f9d80`

Any source owner, visibility, signature, line, digest, wrapper, or callback-condition mismatch fails closed.

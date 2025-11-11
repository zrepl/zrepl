The goal of this document is to describe what **logical steps** zrepl takes to replicate ZFS filesystems.
Note that the actual code executing these steps is spread over the `endpoint.{Sender,Receiver}` RPC endpoint implementations. The code in `replication/{driver,logic}` (mostly `logic.Step.doReplication`) drives the replication and invokes the `endpoint` methods (locally or via RPC).
Hence, when trying to map algorithm to implementation, use the code in package `replication` as the entrypoint and follow the RPC calls.


* The [Single Replication Step](#zrepl-algo-single-step) algorithm is implemented as described
  * step holds are implemented as described
  * step bookmarks rely on bookmark cloning, which is WIP in OpenZFS (see respective TODO comments)
  * the feature to hold receive-side `to` immediately after replication is used to move the `last-received-hold` forward
    * this is a little more than what we allow in the algorithm (we only allow specifying a hold tag whereas moving the `last-received-hold` is a little more complex)
  * the algorithm's sender-side callback is used move the `zrepl_CURSOR` bookmark to point to `to`
* The `zrepl_CURSOR` bookmark and the `last-received-hold` ensure that future replication steps can always be done using incremental replication:
  * Both reference / hold the last successfully received snapshot `to`.
  * Thus, `to` or `#zrepl_CURSOR_G_${to.GUID}_J_${jobid}` can always be used for a future incremental replication step `to => future-to`:
    * incremental send will be possible because `to` doesn't go away because we claim ownership of the `#zrepl_CURSOR` namespace
    * incremental recv will be possible iff
      * `to` will still be present on the recv-side because of `last-received-hold`
      * the recv-side fs doesn't get newer snapshots than `to` in the meantime
        * guaranteed because the zrepl model of the receiver assumes ownership of the filesystems it receives into
          * if that assumption is broken, future replication attempts will fail with a conflict
* The [Algorithm for Planning and Executing Replication of an Filesystems](#zrepl-algo-filesystem) is a design draft and not used.
  However, there were some noteworthy lessons learned when implementing the algorithm for a single step:
  * In order to avoid leaking `step-hold`s and `step-bookmarks`, if the replication planner is invoked a second time after a replication step (either initial or incremental) has been attempted but failed to completed, the replication planner must
    * A) either guarantee that it will resume that replication step, and continue as if nothing happened or
    * B) release the step holds and bookmarks and clear the partially received state on the sending side.
  * Option A is what we want to do: we use the step algorithm to achieve resumability in the first place!
  * Option B is not done by zrepl except if the sending side doesn't support resuming.
    In that case however, we need not release any holds since the behavior is to re-start the send
    from the beginning.
  * However, there is one **edge-case to Option A for initial replication**:
    * If initial replication "`full a`" fails without leaving resumable state, the step holds on the sending side are still present, which makes sense because
      * a) we want resumability and
      * b) **the sending side cannot immediately be informed post-failure whether the initial replication left any state that would mandate keeping the step hold**, because the network connection might have failed.
    * Thus, the sending side must keep the step hold for "`full a`" until it knows more.
    * In the current implementation, it knows more when the next replication attempt is made, the planner is invoked, the diffing algorithm run, and the `HintMostRecentCommonAncestor` RPC is sent by the active side, communicating the most recent common version shared betwen sender and receiver.
    * At this point, the sender can safely throw away any step holds with CreateTXG's older than that version.
  * **The `step-hold`, `step-bookmark`, `last-received-hold` and `replication-cursor` abstractions are currently local concepts of package `endpoint` and not part of the replication protocol**
    * This is not necessarilty the best design decision and should be revisited some point:
      * The (quite expensive) `HintMostRecentCommonAncestor` RPC impl on the sender would not be necessary if step holds were part of the replication protocol:
        * We only need the `HintMostRecentCommonAncestor` info for the aforementioned edge-case during initial replication, where the receive is aborted without any partial received state being stored on the receiver (due to network failure, wrong zfs invocation, bad permissions, etc):
        * **The replication planner does not know about the step holds, thus it cannot deterministically pick up where it left of (right at the start of the last failing initial replication).**
        * Instead, it will seem like no prior invocation happened at all, and it will apply its policy for initial replication to pick a new `full b != full a`, **thereby leaking the step holds of `full a`**.
        * In contrast, if the replication planner created the step holds and knew about them, it could use the step holds as an indicator where it left off and re-start from there (of course asserting that the thereby inferred step is compatible with the state of the receiving side).
      * (What we do in zrepl right now is to hard-code the initial replication policy, and hard-code that assumption in `endpoint.ListStale` as well.)
      * The cummulative cleanup done in `HintMostRecentCommonAncestor` provides a nice self-healing aspect, though.

* We also have [Notes on Planning and Executing Replication of Multiple Filesystems](#zrepl-algo-multiple-filesystems-notes)

---

<a id="zrepl-algo-single-step"></a>
## Algorithm for a Single Replication Step

The algorithm described below describes how a single replication step must be implemented.
A *replication step* is a full send of a snapshot `to` for initial replication, or an incremental send (`from => to`) for incremental replication.

The algorithm **ensures resumability** of the replication step in presence of

* uncoordinated or unsynchronized destruction of the filesystem, snapshots, or bookmarks involved in the replication step
* network failures at any time
* other instances of this algorithm executing the same step in parallel (e.g. concurrent replication to different destinations)

To accomplish this goal, the algorithm **assumes ownership of parts of the ZFS hold tag namespace and the bookmark namespace**:
* holds with prefix `zrepl_STEP` on any snapshot are reserved for zrepl
* bookmarks with prefix `zrepl_STEP` are reserved for zrepl

Resumability of a step cannot be guaranteed if these sub-namespaces are manipulated through software other than zrepl.

Note that the algorithm **does not ensure** that a replication *plan*, which describes a *set* of replication steps, can be carried out successfully.
If that is desirable, additional measures outside of this algorithm must be taken.

---

### Definitions:

#### Step Completion & Invariants

The replication step (full `to` send or `from => to` send) is *complete* iff the algorithm ran to completion without errors or a permanent non-network error is reported by sender or receiver.

Specifically, the algorithm may be invoked with the same `from` and `to` arguments, and potentially a `resume_token`, after a temporary (like network-related) failure:
**Unless permanent errors occur, repeated invocations of the algorithm with updated resume token will converge monotonically (but not strictly monotonically) toward completion.**

Note that the mere existence of `to` on the receiving side does not constitute completion, since there may still be post-recv actions to be performed on sender and receiver.

#### Job and Job ID
This algorithm supports that *multiple* instance of it run in parallel on the *same* step (full `to` / `from => to` pair).
An exemplary use case for this feature are concurrent replication jobs that replicate the same dataset to different receivers.

**We require that all parallel invocations of this algorithm provide different and unique `jobid`s.**
Violation of `jobid` uniqueness across parallel jobs may result in interference between instances of this algorithm, resulting in potential compromise of resumability.

After a step is *completed*, `jobid` is guaranteed to not be encoded in on-disk state.
Before a step is completed, there is no such guarantee.
Changing the `jobid` before step completion may compromise resumability and may leak the underlying ZFS holds or step bookmarks (i.e. zrepl won't clean them up)
Note the definition of *complete* above.

#### Step Bookmark

A step bookmark is our equivalent of ZFS holds, but for bookmarks.<br/>
A step bookmark is a ZFS bookmark whose name matches the following regex:

```
STEP_BOOKMARK = #zrepl_STEP_bm_G_([0-9a-f]+)_J_(.+)
```

- capture group `1` must be the guid of `zfs get guid ${STEP_BOOKMARK}`, encoded hexadecimal (without leading `0x`) and fixed-length (i.e. padded with leading zeroes)
- capture group `2` must be equal to the `jobid`

### Algorithm

INPUT:

* `jobid`
* `from`: snapshot or bookmark: may be nil for full send
* `to`: snapshot, must never be nil
* `resume_token` (may be nil)
* (`from` and `to` must be on the same filesystem)

#### Prepare-Phase

Send-side: make sure `to` and `from` don't go away

- hold `to` using `idempotent_hold(to, zrepl_STEP_J_${jobid})`
- make sure `from` doesn't go away:
  - if `from` is a snapshot: hold `from` using `idempotent_hold(from, zrepl_STEP_J_${jobid})`
  - else `idempotent_step_bookmark(from)` (`from` is a bookmark)
    - PERMANENT ERROR if this fails (e.g. because `from` is a bookmark whose snapshot no longer exists and we don't have bookmark copying yet (ZFS feature is in development) )
      - Why? we must assume the given bookmark is externally managed (i.e. not in the )
      - this means the bookmark is externally created bookmark and cannot be trusted to persist until the replication step succeeds
      - Maybe provide an override-knob for this behavior

Recv-side: no-op

- `from` cannot go away once we received enough data for the step to be resumable:
    ```text
    # do a partial incremental recv @a=>@b (i.e. recv with -s, and abort the incremental recv in the middle)
    # try destroying the incremental source on the receiving side
    zfs destroy p1/recv@a
    cannot destroy 'p1/recv@a': snapshot has dependent clones
    use '-R' to destroy the following datasets:
    p1/recv/%recv
    # => doesn't work, because zfs recv is implemented as a `clone` internally, that's exactly what we want
    ```

- if recv-side `to` exists, goto cleanup-phase (no replication to do)
  
- `to` cannot be destroyed while being received, because it isn't visible as a snapshot yet (it isn't yet one after all)

#### Replication Phase

Attempt the replication step:
start `zfs send` and pipe its output into `zfs recv` until an error occurs or `recv` returns without error.

Let us now think about interferences that could occur during this phase, and ensure that none of them compromise the goals of this algorithm, i.e., monotonic convergence toward step completion using resumable send & recv.

**Safety from External Pruning**
We are safe from pruning during the replication step because we have guarantees that no external action will destroy send-side `from` and `to`, and recv-side `to` (for both snapshot and bookmark `from`s)<br/>

**Safety In Presence of Network Failures During Replication**
Network failures during replication can be recovered from using resumable send & recv:

- Network failure before the receive-side could stored data:
  - Send-side `from` and `to` are guaranteed to be still present due to zfs holds
  - recv-side `from` may no longer exist because we don't `hold` it explicitly
    - if that is the case, ERROR OUT, step can never complete
  - If the step planning algorithm does not include the step, for example because a snapshot filter configuration was changed by the user inbetween which hides `from` or `to` from the second planning attempt: **tough luck, we're leaking all holds**
- Network failure during the replication
  - send-side `from` and `to` are still present due to zfs holds
  - recv-side `from` is still present because the partial receive state prevents its destruction (see prepare-phase)
  - if recv-side has a resume token, the resume token will continue to work on the sender because `from`s and `to` are still present
- Network failure at the end of the replication step stream transmission
  - Variant A: failure from the sender's perspective, success from the receiver's perspective
    - receive-side `to` doesn't have a hold and could be destroyed anytime
    - receive-side `from` doesn't have a hold and could be destroyed anytime
    - thus, when the step is invoked again, pattern match `(from_exists, to_exists)`
      - `(true, true)`: prepare-phase will early-exit
      - `(false,true)`: prepare-phase will error out bc. `from` does not exist
      - `(true, false)`: entire step will be re-done
        - FIXME monotonicity requirement does not hold
      - `(false, false)`: prepare-phase will error out bc. `from` does not exist
  - Variant B: success from the sender's perspective, failure from the receiver's perspective
    - No idea how this would happen except for bugs in error reporting in the replication protocol
    - Misclassification by the sender, most likely broken error handling in the sender or replication protocol
    - => the sender will release holds and move the replication cursor while the receiver won't => tough luck

If the RPC used for `zfs recv` returned without error, this phase is done.
(Although the *replication step* (this algorithm) is not yet *complete*, see definition of complete).


#### Cleanup-Phase

At the end of this phase, all intermediate state we built up to support the resumable replication of the step is destroyed.
However, consumers of this algorithm might want to take advantage of the fact that we currently still have holds / step bookmarks.

##### Recv-side: Optionally hold `to` with a caller-defined tag

Users of the algorithm might want to depend on `to` being available on the receiving side for a longer duration than the lifetime of the current step's algorithm invocation.
For reasons explained in the next paragraph, we cannot guarantee that we have a `hold` when `recv` exists. If that were the case, we could take our time to provide a generalized callback to the user of the algorithm, and have them do whatever they want with `to` while we guarantee that `to` doesn't go away through the hold. But that functionality isn't available, so we only provide a fixed operation right after receive: **take a `hold` with a tag of the algorithm user's choice**. That's what's needed to guarantee that a replication plan, consisting of multiple consecutive steps, can be carried out without a receive-side prune job interfering by destroying `to`. Technically, this step is racy, i.e., it could happen that `to` is destroyed between `recv` and `hold`. But this is a) unlikely and b) not fatal because we can detect that hold fails because the 'dataset does not exist` and re-do the entire transmission since we still hold send-side `from` and `to`, i.e. we just lose a lot of work in rare cases.

So why can't we have a `hold` at the time `recv` exits?
It seems like [`zfs send --holds`](https://github.com/zfsonlinux/zfs/commit/9c5e88b1ded19cb4b19b9d767d5c71b34c189540) could be used to send the send-side's holds to the receive-side. But that flag always sends all holds, and `--holds` is implemented in user-space libzfs, i.e., doesn't happen in the same txg as the recv completion. Thus, the race window is in fact only smaller (unless we oversaw some synchronization in userland zfs).
But if we're willing to entertain the idea a little further, we still hit the problem that `--holds` sends _all_ holds, whereas our goal is to _temporarily_ have >= 1 hold that we own until the callback is done, and then release all of the received holds so that no holds created by us exist after this algorithm completes.
Since there could be concurrent `zfs hold` invocations on the sender and receiver while this algorithm runs, and because `--holds` doesn't provide info about which holds were sent, we cannot correctly destroy _exactly_ those holds that we received.

##### Send-side: callback to consumer, then cleanup
We can provide the algorithm user with a generic callback because we have holds / step bookmarks for `to` and `from`, respectively.

Example use case for the sender-side callback is the replication cursor, see algorithm for filesystem replication below.

After the callback is done: cleanup holds & step bookmark:

- `idempotent_release(to, zrepl_STEP_J_${jobid})`
- make sure `from` can now go away:
  - if `from` is a snapshot: `idempotent_release(from, zrepl_STEP_J_${jobid})`
  - else `idempotent_step_bookmark_destroy(from)`
    - if `from` fulfills the properties of a step bookmark: destroy it
    - otherwise: it's a bookmark not created by this algorithm that happened to be used for replication: leave it alone
      - that can only happen if user used the override knob

Note that "make sure `from` can now go away" is the inverse of "Send-side: make sure `to` and `from` don't go away". Those are relatively complex operations and should be implemented in the same file next to each other to ease maintainability.

---

### Notes & Pseudo-APIs used in the algorithm

- `idempotent_hold(snapshot s, string tag)` like zfs hold, but doesn't error if hold already exists
- `idempotent_release(snapshot s, string tag)` like zfs hold, but doesn't error if hold already exists
- `idempotent_step_bookmark(snapshot_or_bookmark b)` creates a *step bookmark* of b
  - determine step bookmark name N
  - if `N` already exists, verify it's a correct step bookmark => DONE or ERROR
  - if `b` is a snapshot, issue `zfs bookmark`
  - if `b` is a bookmark:
    - if bookmark cloning supported, use it to duplicate the bookmark
    - else ERROR OUT, with an error that upper layers can identify as such, so that they are able to ignore the fact that we couldn't create a step bookmark 
- `idempotent_destroy(bookmark #b_$GUID, of a snapshot s)` must atomically check that `$GUID == s.guid` before destroying s
- `idempotent_bookmark(snapshot s, $GUID, name #b_$GUID)` must atomically check that `$GUID == s.guid` at the time of bookmark creation
- `idempotent_destroy(snapshot s)` must atomically check that zrepl's `s.guid` matches the current `guid` of the snapshot (i.e. destroy by guid)


<a id="zrepl-algo-filesystem"></a>
## Algorithm for Planning and Executing Replication of a Filesystems (planned, not implemented yet)

This algorithm describes how a filesystem or zvol is replicated in zrepl.
The algorithm is invoked with a `jobid`, `sender`, `receiver`, a sender-side `filesystem path`.
It builds a diff between the sender and receiver filesystem bookmarks+snapshots and determines whether a replication conflict exists or whether fast-forward replication using full or incremental sends is possible.
In case of conflict, the algorithm errors out with a conflict description that can be used to manually or automatically resolve the conflict.
Otherwise, the algorithm builds a list of replication steps that are then worked on sequentially by the "Algorithm for a Single Replication Step".

The algorithm ensures that a plan can be executed exactly as planned by acquiring appropriate zfs holds.
The algorithm can be configured to retry a plan when encountering non-permanent errors (e.g. network errors).
However, permanent errors result in the plan being cancelled.

Regardless of whether a plan can be executed to completion or is cancelled, the algorithm guarantees that leftover artifacts (i.e. holds) of its invocation are cleaned up.
However, since network failure can occur at any point, there might be stale holds on sending or receiving side after a crash or other error.
These will be cleaned up on a subsequent invocation of this algorithm with the same `jobid`.

The algorithm is fit to be executed in parallel from the same sender to different receivers.
To be clear: replicating in parallel from the same sender to different receivers is supported.
But one instance of the algorithm assumes ownership of the `filesystem path` on the receiver.

The algorithm reserves the following sub-namespaces:
* zfs hold: `zrepl_FS`
* bookmark names: `zrepl_FS`

### Definitions

#### ZFS Filesystem Path
We refer to the name of a ZFS filesystem or volume as a *filesystem path*, sometimes just *path*.
The name includes the pool name.

#### Sender and Receiver
Sender and Receiver are passed as RPC stubs to the algorithm.
One of those RPC stubs will typically be local, i.e., call methods on a struct in the same address space.
The other RPC stub may invoke actual calls over the network (unless local replication is used).
The algorithm does not make any assumption about which object is local or an actual stub.
This design decouples the replication logic (described in this algorithm) from the question which side (sender or receiver) initiates the replication.

### Algorithm

**Cleanup Previous Invocations With Same `jobid`** by scanning the filesystem's list of snapshots and step bookmarks, and destroying any which was created by this algorithm when it was invoked with `jobid`.<br/>
TODO HOW

**Build a fast-forward list of replication steps** `STEPS`.
`STEPS` may contain an optional initial full send, and subsequent incremental send steps.
`STEPS[0]` may also have a resume token.
If fast-forward is not possible, produce a conflict description and ERROR OUT.<br/>
TODOs:
- make it configurable what snapshots are included in the list (i.e. every one we see, only most recent, at least one every X hours, ...)

**Ensure that we will be able to carry out all steps** by acquiring holds or fsstep bookmarks on the sending side
- `idempotent_hold([s.to for s in STEPS], zrepl_FS_J_${jobid})`
- `if STEPS[0].from != nil: idempotent_FSSTEP_bookmark(STEPS[0].from, zrepl_FSSTEP_bm_G_${STEPS[0].from.guid}_J_${jobid})` 
  
**Determine which steps have not been completed (`uncompleted_steps`)** (we might be in an second invocation of this algorithm after a network failure and some steps might already be done):
- `res_tok := receiver.ResumeToken(fs)`
- `rmrfsv := receiver.MostRecentFilesystemVersion(fs)`
- if `res_tok !=  nil`: ensure that `res_tok` has a corresponding step in `STEPS`, otherwise ERROR OUT
- if `rmrfsv != nil`: ensure that `res_tok` has a corresponding step in `STEPS`, otherwise ERROR OUT
- if `(res_token != nil && rmrfsv != nil)`: ensure that `res_tok` is the subsequent step to the one we found for `rmrfsv`
- if both are nil, we are at the beginning, `uncompleted_steps = STEPS` and goto next block
- `rstep := if res_tok != nil { res_tok } else { rmrfsv }`
- `uncompleted_steps := STEPS[find_step_idx(STEPS, rstep).expect("must exist, checked above"):]`
  - Note that we do not explicitly check for the completion of prior replication steps.
    All we care about is what needs to be done from `rstep`.
  - This is intentional and necessary because we cumulatively release all holds and step bookmarks made for steps that precede a just-completed step (see next paragraph)

**Execute uncompleted steps**<br/>
Invoke the "Algorithm for a Single Replication Step" for each step in `uncompleted_steps`.
Remember to pass the resume token if one exists.

In the wind-down phase of each replication step `from => to`, while the per-step algorithm still holds send-side `from` and `to`, as well as recv-side `to`:

- Sending side: **Idempotently move the replication cursor to `to`.**
  - Why: replication cursor tracks the sender's last known replication state, outlives the current plan, but is required to guarantee that future invocations of this algorithm find an incremental path.
  - Impl for 'atomic' move: have two cursors (fixes [#177](https://github.com/LyingCak3/zrepl/issues/177))
      - Idempotently cursor-bookmark `to` using `idempotent_bookmark(to, to.guid, #zrepl_replication_cursor_G_${to.guid}_J_${jobid})`
      - Idempotently destroy old cursor-bookmark of `from` `idempotent_destroy(#zrepl_replication_cursor_G_${from.guid}_J_${jobid}, from)`
    - If `from` is a snapshot, release the hold on it using `idempotent_release(from,  zrepl_J_${jobid})`
    - FIXME: resumability if we crash inbetween those operations (or scrap it and wait for channel programs to bring atomicity)

- Receiving side: **`idempotent_hold(to, zrepl_FS_J_${jobid})` because its going to be the next step's `from`**
  - As discussed in the section on the per-step algorithm, this is a feature provided to us by the per-step algorithm.

- Receiving side + Sending side (order doesn't matter): Make sure all holds & step bookmarks made by this plan on already replicated snapshots are released / destroyed:
  - `idempotent_release_prior_and_including(from, zrepl_FS_TODO)`
  - `idempotent_step_bookmark_destroy_prior_and_including(from, zrepl_FS_TODO)`

**Cleanup** receiving and sending side (order doesn't matter)

  - `idempotent_release_prior_and_including(STEPS[-1].to, zrepl_FS_TODO)`
  - `idempotent_step_bookmark_destroy_prior_and_including(STEPS[-1].from, zrepl_FS_TODO)`

### Notes

- `idempotent_FSSTEP_bookmark` is like `idempotent_STEP_bookmark`, but with prefix `zrepl_FSSTEP` instead of `zrepl_STEP`


<a id="zrepl-algo-multiple-filesystems-notes"></a>

## Notes on Planning and Executing Replication of Multiple Filesystems

### The RPC Interfaces Uses Send-side Filesystem Names to Identify a Filesystem

The model of the receiver includes the assumption that it will `Receive` all snapshot streams sent to it into filesystems with paths prefixed with `root_fs`.
This is useful for separating filesystem path namespaces for multiple clients.
The receiver hides this prefixing in its RPC interface, i.e., when responding to `ListFilesystems` rpcs, the prefix is removed before sending the response.
This behavior is useful to achieve symmetry in this algorithm: we do not need to take the prefixing into consideration when computing diffs.
For the receiver, it has the advantage that `Receive` rpc can enforce the namespacing and need not trust this algorithm to apply it correctly.

The receiver is also allowed (and may need to) do implement other filtering / namespace transformations.
For example, when only `foo/a/b` has been received, but not `foo` nor `foo/a`, the receiver must not include the latter two in its `ListFilesystems` response.
The current zrepl receiver endpoint implementation uses the `zrepl:placeholder` property to mark filesystems `foo` and `foo/a` as placeholders, and filters out such filesystems in the `ListFilesystems` response.
Another approach would be to have a flat list of received filesystems per sender, and have a separate table that associates send-side names and receive-side filesystem paths.

Similarly, the sender is allowed to filter the output of its RPC interface to hide certain filesystems or snapshots.

#### Consequences of the Above Design

Namespace filtering and transformations of filesystem paths on sender and receiver are user-configurable.
Both ends of the replication setup have their own config, and do not coordinate config changes.
This results in a number of challenges:

- A sender might change its filter to allow additional filesystems to be replicated.
  For example, where `foo/a/b` was initially allowed, the new filter might allow `foo` and `foo/a` as well.
  If the receiver uses the `zrepl:placeholder` approach as sketched out above, this means that the receiver will need to replace the placeholder filesystems `$root_fs/foo` and `$root_fs/foo/a` with the incoming full sends of `foo` and `foo/a`.

- Send-side renames cannot be handled efficiently because send-side rename effectively changes the filesystem identity because we use its name.

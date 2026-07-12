Here's the bio lifecycle from submission through driver dispatch:

## **1. Submission → Accounting**
**submit_bio()** — blk-core.c:952
- Entry point; does accounting (reads/writes VM stats), sets I/O priority
- Calls submit_bio_noacct()

## **2. Validation & Checks**
**submit_bio_noacct()** — blk-core.c:817
- Validates queue support (NOWAIT, crypto, read-only), operation type
- Checks zone constraints, flushes, atomic writes
- Applies I/O throttling (blk_throtl_bio)
- Calls submit_bio_noacct_nocheck()

## **3. Dispatch Routing (stacked layers)**
**submit_bio_noacct_nocheck()** — blk-core.c:762
- Sets up bio_list tracking (for stacked driver recursion)
- Routes to __submit_bio_noacct() (legacy drivers) or __submit_bio_noacct_mq() (modern MQ)

## **4. Dispatch to MQ Layer**
**__submit_bio()** — blk-core.c:670
- Final router: calls **blk_mq_submit_bio()** (MQ) or driver->submit_bio() (legacy)

---

## **5. SPLIT/LIMITS**
**blk_mq_submit_bio()** — blk-mq.c:3093 (start)
- **__bio_split_to_limits()** — blk.h:429
  - Splits large bios to respect device limits (max_sectors, segments, etc.)
  - Returns split bio; remainder queued for resubmission

## **6. MERGE**
**blk_mq_attempt_bio_merge()** — blk-mq.c:3003
- Tries plug merge: **blk_attempt_plug_merge()** — blk-merge.c:1082
  - Merges with previous requests in current plug
- Falls back to scheduler merge: **blk_mq_sched_bio_merge()** — blk-mq-sched.c:335
  - Merges with requests already in elevator queue
- Returns true if merged (bio consumed)

## **7. PLUG/BATCHING**
**blk_add_rq_to_plug()** — blk-mq.c:1377
- Adds request to thread's plug list (batches multiple requests)
- Flushes early if plug full or size threshold hit: **blk_mq_flush_plug_list()** — blk-mq.c:2938
  - Dispatches batched requests to driver in one go

## **8. SCHEDULER/INSERT**
**blk_mq_insert_request()** — blk-mq.c:2592
- If elevator active: **q->elevator->type->ops.insert_requests()** (calls scheduler like kyber/bfq)
- If no elevator: adds to hw context's software queue
- Passthrough/flush requests bypass to dispatch queue (hctx->dispatch)

## **9. DISPATCH (HW Queue)**
**blk_mq_try_issue_directly()** — blk-mq.c:2737
- Checks if hardware queue is stopped/quiesced
- Gets budget and driver tag via blk_mq_get_budget_and_tag()
- Tries direct issue via **__blk_mq_issue_directly()** or falls back to dispatch queue

## **10. DRIVER**
**__blk_mq_issue_directly()** — blk-mq.c:2679
- Calls **q->mq_ops->queue_rq(hctx, &bd)** — final hand-off to driver
- Driver returns BLK_STS_OK, BLK_STS_RESOURCE (requeue), or error
- Updates dispatch busy state; requeued requests go to hctx->dispatch

---

**Key throttle points:** limits (5), merge (6), scheduler queue capacity (8), dispatch queue depth (9), hardware resources (10).
---
title: REST API 예시 셋 — 묻기, 정의 고치기, 붙잡힌 흐름 풀기를 끝까지
kind: guide
status: active
follows:
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
---
# REST API 예시 셋

규칙([REST API 설계](rest-api.md))과 길([모델·받기·실행·정의](rest-api-resources-data.md), [KG·관측·역량 질문·권한·바탕](rest-api-resources-graph.md), [더한 기능](rest-api-resources-additions.md))이 실제로 어떻게 맞물리는지 지어낸 장면 셋으로 따라가요. 장면은 처음부터 끝까지 요청과 응답을 적어요. 모든 이름, ID, 숫자는 지어낸 거예요.

> **길과 칸은 다음 변경의 OpenAPI 문서에서 굳어요.** KG에 든 길은 `/v1/kgs/{kg}/...`이고(운영의 KG는 `main`) 행위는 `:행위` 표기예요.

## 1. 영향 범위를 묻고 믿어도 되는지 확인하기 (읽기)
화면의 운영자 `user_a`(역할 `viewer`)이 "스위치 `sw-0417`이 죽으면 어디까지 번지나"를 알고 싶어요.

**1) 로그인과 "나".** 화면은 SSO에서 인증 코드 흐름과 PKCE로 증표를 받았어요. 요청마다 싣고, 먼저 "나"를 불러요.

```
GET /v1/me          Authorization: Bearer <증표>
→ 200 {"data": {"subject": {"kind": "person", "subject_id": "sub_21"},
                "grants": [{"role": "viewer", "scope": {"kg": "main", "object_types": ["Server", "Switch", "Service"]}}], "bulk_read": false,
                "allowed_actions": [{"action": "propose", "allowed": false, "reason_code": "forbidden.role"}],
                "capabilities": {"api_version": "v1", "features": ["competency", "events_stream"]}}}
```

화면은 이 답으로 "제안" 단추를 끄고 까닭(`forbidden.role`)을 보여요(`viewer`에게 없는 일). 끄는 것은 편의이고, 서버는 누르면 어차피 거절해요.

**2) 영향 범위를 물어요.** 닫힌 질의는 POST의 물음 자원이에요. 상태를 바꾸지 않으니 `Idempotency-Key`가 없어요.

```
POST /v1/kgs/main/queries:impact
{"dead": ["obj_sw0417"], "mode": "both", "view": "as_observed", "shape": "summary", "limit": 2}
→ 200
{"data": {"facts_only": {"counts_by_result": {"down": 1, "degraded": 4}},
          "with_assumptions": {"counts_by_result": {"down": 3, "degraded": 9}, "items": [
             {"object_id": "obj_srv_a", "object_type": "Server", "result": "down", "via": "obj_sw0417"},
             {"object_id": "obj_svc_b", "object_type": "Service", "result": "degraded", "via": "obj_srv_a"}]}},
 "meta": {"as_of": {"kg": "kg_main_01", "generation": 1, "change_seq": 88231, "sealed_version": "kv_0192", "pinned": null, "volatile_as_of": "current"},
          "served_by": {"role": "engine"},
          "freshness": {"worst_source_id": "src_switch_inventory", "oldest_age_seconds": 97200, "state": "stale"},
          "evidence": {"min_grade": "assumed", "counts": {"fact": 5, "assumed": 7, "unknown": 0}},
          "result_complete": {"total": 12, "returned": 2, "truncated": true, "reason": "limit", "next_cursor": "c_71Kd"},
          "hidden": {"has_hidden": true},
          "health_hint": {"has_unhealthy_inputs": true, "worst_state": "affected_upstream", "total": 1},
          "trace_id": "tr_0a9"}}
```

화면이 봉투에서 읽는 것:
- `freshness.state: stale`: 스위치 소스의 마지막 수신이 27시간 전이에요. 화면은 "묵은 구성 기준"이라고 쓰고, 이 문구는 화면의 말이에요.
- `evidence.counts`: 사실 5, 가정 7이에요. 사실만으로는 하나만 죽고, 가정을 넣으면 셋이 죽어요.
- `hidden.has_hidden: true`: 번지는 길에 `user_a`이 못 읽는 객체가 있어요. 범위가 전체가 아닌 주체에게는 수 대신 있음만 줘서(설정으로 수를 열 수 있어요) 남의 객체를 헤아리지 못해요. 내용은 안 줘요. `result_complete.total`도 가려진 것을 뺀 수예요.
- `result_complete.truncated: true`: 12개 중 2개만 왔어요. `next_cursor`로 이어 받아요.

**3) 다음 쪽.** 같은 입력에 `cursor`를 더해요.

```
POST /v1/kgs/main/queries:impact
{"dead": ["obj_sw0417"], "mode": "both", "view": "as_observed", "shape": "items", "limit": 50, "cursor": "c_71Kd"}
→ 200 {"data": {...나머지 10개...}, "meta": {..., "result_complete": {"total": 12, "returned": 10, "truncated": false, "next_cursor": null}}}
```

`cursor`는 KG, 세대, `change_seq` 88231을 품고 있어서, 그사이 KG가 바뀌어도 같은 자리에서 이어져요. 그사이 시점 복구로 세대가 바뀌었으면 `conflict.generation_changed`(409)가 와요. 새 변경을 보려면 `cursor` 없이 다시 물어요.

**4) 왜 묵었나.** `health_hint`가 가리킨 자원의 건강을 읽어요.

```
GET /v1/kgs/main/health/source/src_switch_inventory?include=cause,readers
→ 200 {"data": {"health_state": "failing", "cause": [{"check": "auth_failed", "since": "2031-04-01T08:10:00Z"}]}, "meta": {...}}
```

**5) 알림을 받아요.** 화면은 건강의 바뀜을 기다려요.

```
GET /v1/events/stream?kinds=health_changed&resource=source:src_switch_inventory   Authorization: Bearer <증표>
id: ev_000812
event: health_changed
data: {"resource": {"kind": "source", "id": "src_switch_inventory"}, "from": "failing", "to": "healthy"}
```

연결이 끊겼다 다시 붙을 때 화면은 마지막으로 받은 `ev_000812`를 `Last-Event-ID`로 보내요. 너무 오래 끊겼다면 `GET /v1/events?since=ev_000812`가 `stale.since_too_old`(410)로 답하고(세대가 바뀐 뒤이면 `conflict.generation_changed`), 그때는 건강을 다시 읽어요.

**6) 한도.** 화면이 짧은 시간에 영향 범위를 연달아 물으면 이래요.

```
→ 429  Retry-After: 30   RateLimit: limit=60, remaining=0, reset=30
{"type": "/v1/problems/rate_limited.compute_budget", "status": 429, "code": "rate_limited.compute_budget",
 "params": {"limit_kind": "compute_budget", "retry_after_seconds": 30, "hint": "narrow_scope"}, "trace_id": "tr_0b2"}
```

화면은 `code`와 `params`로 자기 글("30초 뒤에 다시 해 주세요")을 만들어요. `RateLimit` 머리의 모양은 초안을 따라요.

## 2. 일정을 고쳐 병합하기 (쓰기, 겹침 방지, 동시 수정, 작업, 승인)
`user_a`과 다른 사람 `user_b`는 둘 다 `editor`이고 소스 `src_inventory`를 범위에 가져요. `user_c`은 그 소스를 범위에 가진 `approver`예요. 목표: 소스의 일정을 6시간마다로 바꿔요.

**1) 브랜치를 열어요.**

```
POST /v1/branches    Idempotency-Key: 8d1c-...
{"name": "inventory-every-6h"}
→ 201 {"data": {"branch_id": "br_12", "base_set_no": 141}}
```

네트워크가 끊겨 화면이 같은 요청을 다시 보내면 같은 `Idempotency-Key`라서 브랜치가 둘 생기지 않고 처음 응답이 `Idempotent-Replayed: true`와 함께 와요.

**2) 항목을 읽고 고쳐요.**

```
GET /v1/branches/br_12/items/itm_88          → 200 ETag: "rev-17"  {"data": {"kind": "schedule", "body": {"every": "PT12H"}}}
PATCH /v1/branches/br_12/items/itm_88        Idempotency-Key: 41ab-...   If-Match: "rev-17"
Content-Type: application/merge-patch+json
{"body": {"every": "PT6H"}}
→ 200 ETag: "rev-18"
```

**3) 동시에 고친 사람이 있을 때.** `user_b`가 같은 항목을 `rev-17`에서 `PT4H`로 고쳐 보내요. 이미 `rev-18`이라 거절돼요.

```
PATCH /v1/branches/br_12/items/itm_88        If-Match: "rev-17"
{"body": {"every": "PT4H"}}
→ 412 {"code": "conflict.version_mismatch", "status": 412, "current_etag": "rev-18", "params": {"changed_fields": ["body.every"]}, "trace_id": "tr_1c"}
```

`user_b`의 화면은 다시 읽고 두 값을 나란히 보여 줘서 `user_b`가 고르게 해요. `If-Match` 없이 보냈다면 428 `invalid_input.precondition_required`예요.

**4) 제안을 만들고 검사해요.**

```
POST /v1/proposals   Idempotency-Key: 77f0-...   {"branch_id": "br_12", "title": "inventory every 6h"}
→ 201 {"data": {"proposal_id": "prop_31", "status": "draft"}}
POST /v1/proposals/prop_31:request-review   Idempotency-Key: 2e9a-...
→ 200 {"data": {"status": "in_review"}}
```

작은 변경이면 1)~4)를 한 번에 할 수 있어요. `POST /v1/proposals:from-items`에 항목의 목록(`{"kind": "schedule", "item_id": "itm_88", "patch": {...}}`)과 `request_review: true`를 주면 브랜치를 열고 항목을 쓰고 제안을 만들고 검토를 올려요. 항목마다 보통의 권한과 검사가 걸려서 승인을 건너뛰지 않아요.

검토 요청 때 낱개의 유효성 검사(`item_valid`)가 돌아요. 값이 틀렸다면 422 `invalid_input.field`와 칸마다의 `errors`가 와요.

**5) 후보 실행(작업).** 새 일정으로 받아 둔 원본 기록을 돌려 KG의 차이를 봐요.

```
POST /v1/proposals/prop_31/candidate-runs   Idempotency-Key: c0d3-...
→ 202 Location: /v1/jobs/job_4d2   {"data": {"job_id": "job_4d2", "state": "queued"}}

GET /v1/jobs/job_4d2   → {"data": {"state": "running", "progress": {"stage": "link", "done": 4, "total": 7, "unit": "stage"}}}
(사건: job_finished)
GET /v1/jobs/job_4d2   → {"data": {"state": "succeeded", "result": {"href": "/v1/candidate-runs/run_9", "expires_at": "2031-04-09T00:00:00Z"}}}
GET /v1/candidate-runs/run_9/diff?limit=20   → 타입별 더함·고침·물러남과 쪽 넘김
```

`user_c`(승인자)도 이 제안을 읽을 수 있어서 같은 작업의 진행을 읽어요. 화면은 진행을 보려고 `GET /v1/jobs/job_4d2`를 되풀이하지 않고 사건의 길(`job_progress`, `job_finished`)을 받아요. 사건 길이 끊기면 `GET /v1/events?since=`로 놓친 것을 메워요.

**6) 막는 까닭을 읽어요.**

```
GET /v1/proposals/prop_31
→ {"data": {"status": "in_review", "blockers": [{"blocker": "approvals_missing", "params": {"item_id": "itm_88", "scope": "source:src_inventory"}}]}}
```

**7) 승인.** `user_a`이 자기가 고친 제안을 승인하려고 하면 거절돼요.

```
POST /v1/proposals/prop_31:approve   (user_a, editor)       → 403 {"code": "permission.role_missing", ...}
(`user_a`이 `approver` 부여도 함께 가졌어도 자기가 고친 제안이라)  → 403 {"code": "permission.self_approval", ...}
POST /v1/proposals/prop_31:approve   (user_c, approver)   Idempotency-Key: 5b2d-...
→ 200 {"data": {"status": "approved", "approvals": [{"item_id": "itm_88", "by": "sub_44"}]}}
```

`user_a`은 제안이 보이는 사람이라 403(`permission.*`)을 받아요. 제안이 보이지 않는 사람이 같은 길을 부르면 404예요.

**8) 줄에 세우고 병합.**

```
POST /v1/proposals/prop_31:enqueue   Idempotency-Key: 9e11-...   → 200 {"data": {"status": "queued"}}
(사건) proposal_state_changed: queued → merged
(사건) definition_merged: {"set_no": 142}
(사건) kg_version_sealed: {"sealed_version": "kv_0193"}
```

줄 맨 앞의 병합과 KG에의 적용은 서버가 해요. 막는 까닭이 있었다면(`conflicts`, `stale` 등) 줄에 세우는 요청이 409 `blocked.proposal`과 `blockers`로 거절돼요.

**9) 되돌리기.** 병합이 잘못이었다면 새 제안으로 되돌려요.

```
POST /v1/proposals/prop_31/reverts   Idempotency-Key: aa01-...   {"emergency": false}
→ 201 {"data": {"proposal_id": "prop_35", "origin": "revert", "reverts_proposal_id": "prop_31"}}
```

그다음은 같은 검토, 승인, 줄이에요. 급한 되돌리기(`emergency: true`)는 `admin`이나, 그 제안이 고친 자리 전부를 범위에 가진 `approver`가 하고(`operator`는 못 해요), 감사 기록에 `admin_override`가 남아요.

## 3. 붙잡힌 흐름을 풀기 (사건, 운영, 작업)
소스 `src_billing_stream`의 데이터셋 `ds_usage`는 `stream`으로 받아요. 받은 모양이 바뀌어 흐름이 붙잡혔어요. 운영자 `user_d`(`operator`)가 일해요.

**1) 알림이 와요.** 사건의 길에서:

```
event: stream_held
data: {"source_id": "src_billing_stream", "dataset_id": "ds_usage", "held_reason": "schema_changed"}
event: alert_opened
data: {"alert_id": "alt_72", "resource": {"kind": "source", "id": "src_billing_stream"}}
```

**2) 지금 상태를 읽어요.**

```
GET /v1/feed-flows?filter[flow_state][equals]=held
→ {"data": [{"flow_id": "flow_3", "source_id": "src_billing_stream", "dataset_id": "ds_usage", "flow_state": "held",
             "held_reason": "schema_changed", "held_since_seq": 5120, "queued_changes": 842, "source_cursor": "p-5962"}], "meta": {...}}
POST /v1/alerts/alt_72:acknowledge   Idempotency-Key: 31b0-...   → 200 {"data": {"state": "acknowledged"}}
```

받는 것은 멈추지 않아서 `queued_changes`가 늘어요(원본 기록에 쌓여요). 다른 소스의 적용은 그대로예요.

**3) 까닭이 정의 탓이에요.** 받은 모양(컬럼 `usage_kwh`가 `usage`로)이 바뀌어 매핑이 안 맞아요. 풀기 전에 정의를 고쳐야 해요. `user_d`는 `operator`라서 정의를 못 고치므로 `editor`에게 알려요. 문제의 `open-proposal` 길로 브랜치와 제안을 열 수 있어요(`editor`가 해요).

```
POST /v1/kgs/main/issues/iss_204:open-proposal   (editor)   Idempotency-Key: d7e0-...
→ 201 {"data": {"proposal_id": "prop_40", "branch_id": "br_20"}}
```

브랜치에서 매핑을 고치고(2의 흐름), 병합돼요. 병합이 확정된 자리부터 새 정의로 흘러요. 병합의 적용은 계보로 닿는 공급과 타입에만 걸려서 닿지 않는 소스의 흐름은 서지 않아요.

**4) 시험해 봐요: 다시 돌리기.** `user_d`는 쌓인 842건이 새 정의에서 어떻게 되는지 소스에 다시 가지 않고 봐요.

```
POST /v1/sync-runs/run_88/replays   Idempotency-Key: 4f4f-...   {"branch_id": "br_20"}
→ 202 Location: /v1/jobs/job_5e1
(끝나면) GET /v1/sync-runs/run_88/stages → 단계별 수: 들어온 842 = 나간 839 + 떨어진 3, dropped_by_reason: {"invalid_value": 3}
GET /v1/sync-runs/run_88/samples?reason=invalid_value → 견본(내용은 읽을 때 가져오고, 민감한 컬럼은 "가려짐")
```

견본은 `operator`, `editor`, `admin`(그 소스의 범위)만 읽어요. `viewer`가 부르면 403이에요. 견본은 MCP로 나가지 않아요. 그 데이터셋이 대는 타입의 읽기 권한도 따지고 감사에 남아요.

**5) 풀기.**

```
POST /v1/feed-flows/flow_3:release    Idempotency-Key: 90c2-...
{"acknowledged_reason": "mapping fixed in prop_40"}
→ 202 Location: /v1/jobs/job_5f7
(사건) job_finished, health_changed: failing → healthy
```

풀기는 쌓인 변경을 받은 순서대로 적용해요. 흐름이 `held`가 아니면 409 `conflict.state_not_allowed`예요. 쌓인 것을 적용하지 않고 지나가려면 `:discard`예요(맞추기를 할 수 있는 소스에서만 돼요. 원본 기록은 안 지워요. 어긋난 것은 다음 맞추기가 바로잡아요).

**6) 같은 일을 명령줄로.** 화면 없이도 같은 일이 돼요. 명령줄 클라이언트는 같은 길을 불러요(도구의 이름은 정하지 않아서 `px`라고 적어요).

```
praxium feed-flow list --filter flow_state=held
praxium sync-run replay run_88 --branch br_20 --wait
praxium feed-flow release flow_3 --reason "mapping fixed in prop_40" --wait
```

## 4. 이 예시들이 보여 주는 규칙
| 규칙 | 어디서 |
|---|---|
| 닫힌 질의는 POST의 물음, 봉투가 한결같음 | 1 |
| 가려진 것은 수로만, 읽을 수 없는 것은 404 | 1, 2 |
| 쪽 넘김은 열쇠(`cursor`)로 | 1 |
| 겹침 방지와 동시 수정 막기 | 2 |
| 오래 걸리는 일은 202와 작업 | 2, 3 |
| 행위는 자원 뒤에 `:행위`로 | 2, 3 |
| KG에 든 것은 `/v1/kgs/{kg}/...` | 1, 3 |
| 사건의 길과 `since`로 이어 받기 | 1, 2, 3 |
| 오류는 `code`와 `params`, 글은 화면이 | 1, 2 |
| 명령줄 클라이언트가 같은 길을 불러요 | 3 |

## 5. 알고 가는 한계
- 장면의 모든 숫자(진행의 단계 수, 기간, 한도의 값)와 이름은 지어낸 거예요.
- `RateLimit` 머리의 형식은 IETF 초안의 모양을 따랐고 바뀔 수 있어요.
- 시스템 계정의 밀어 넣기(`ingester`)와 운영 행위의 승인 장면은 [더한 기능의 길](rest-api-resources-additions.md)의 표로 대신해요.
- 2의 7)에서 한 사람이 두 역할을 가지는 경우의 거절(`permission.self_approval`)은 권한 설계 4절의 규칙(자기가 고친 것은 승인하지 못해요)을 따라요.

## 6. 관련 결정과 요구사항
- ADR-0016, ADR-0010(봉투), ADR-0011(붙잡힌 흐름), ADR-0012(권한), ADR-0013(관측), ADR-0014(정의).
- REQ-143, REQ-147, REQ-148, REQ-149.
